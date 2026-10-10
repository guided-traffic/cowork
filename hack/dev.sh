#!/usr/bin/env bash
# make dev: the whole development stack in one terminal, to watch the UI while it is built.
#
# PostgreSQL (make postgres-up), Silo (make minio-up) and Dex (make dex-up) in containers, the
# backend built from source on :8080 with the local administrator `dev` (docs/adr/0032) and Dex as
# its identity provider (docs/adr/0029 D3), the group mapping team-red -> member and, on the first
# run, demo data in the team `dev`, and ng serve on https://localhost:4200 (a self-signed
# certificate the browser asks about once), which reloads the page on every
# saved change. The browser logs in like on an installation: as `dev` with the development-only
# password below, or with "Sign in with Dex" as a user of hack/dex/config.yaml (docs/adr/0038 D2,
# D4). Ctrl-C stops the backend and the dev server; the containers keep their data (make
# postgres-down minio-down removes it; Dex keeps none). The backend log is in .dev/backend.log.
set -euo pipefail
cd "$(dirname "$0")/.."

POSTGRES_PORT=${POSTGRES_PORT:-5432}
MINIO_PORT=${MINIO_PORT:-9000}
MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY:-cowork}
MINIO_SECRET_KEY=${MINIO_SECRET_KEY:-cowork-secret}
DEX_PORT=${DEX_PORT:-5556}
BUCKET=cowork-dev
STATE=.dev
# Development-only credentials (docs/adr/0038 D4): meaningless outside localhost.
ADMIN_USER=${COWORK_DEV_ADMIN:-dev}
ADMIN_PASSWORD=${COWORK_DEV_ADMIN_PASSWORD:-dev-only-cowork}
# The client secret and the users' password hack/dex/config.yaml holds, development-only as well.
DEX_CLIENT_SECRET=cowork-dev-dex-secret
DEX_PASSWORD=dev-only-dex
# HTTPS with the Angular CLI's self-signed certificate: Safari stores no Secure cookie for
# http://localhost, and the session cookie is Secure everywhere (docs/adr/0031 D2).
UI=https://localhost:4200
DB_APP="postgres://cowork_app:cowork_app@localhost:$POSTGRES_PORT/cowork?sslmode=disable"
DB_OWNER="postgres://cowork_owner:cowork_owner@localhost:$POSTGRES_PORT/cowork?sslmode=disable"
DB_ADMIN="postgres://postgres:postgres@localhost:$POSTGRES_PORT/cowork?sslmode=disable"

step() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }

for port in 8080 4200; do
	if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "port $port is in use; stop what listens there (lsof -nP -iTCP:$port -sTCP:LISTEN) and run make dev again"
		exit 1
	fi
done

mkdir -p "$STATE"
chmod 700 "$STATE"

step "PostgreSQL, Silo and Dex"
make -s dev-up POSTGRES_PORT="$POSTGRES_PORT" MINIO_PORT="$MINIO_PORT" DEX_PORT="$DEX_PORT"
code=$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" \
	--user "$MINIO_ACCESS_KEY:$MINIO_SECRET_KEY" -X PUT "http://localhost:$MINIO_PORT/$BUCKET")
case "$code" in 200 | 409) ;; *) echo "creating the bucket $BUCKET answered $code" && exit 1 ;; esac

step "the backend, built from source"
make -s migrate POSTGRES_PORT="$POSTGRES_PORT"
(cd backend && go build -o "../$STATE/cowork" ./cmd/cowork)

step "the person dev with the team dev, a second person, and a token for the demo data"
seed() { (cd backend && COWORK_DEV_SEED_DATABASE_URL="$DB_ADMIN" go run ./test/devseed "$@"); }
token=$(seed -agent=false | awk '/^token:/ {print $2}')
[ -n "$token" ] || { echo "make dev-seed printed no token"; exit 1; }
(umask 077 && printf '%s\n' "$token" >"$STATE/token")
seed -username sam -agent=false >/dev/null

# The server key stays the same across restarts: list cursors and the login throttle's address
# hashes outlive a restart (sessions are database rows and outlive it anyway).
[ -s "$STATE/session-key" ] || (umask 077 && openssl rand -base64 32 >"$STATE/session-key")
# The chat of the UI talks to a local LM Studio when it answers on :1234 and lists the model
# (COWORK_DEV_CHAT_MODEL, an MLX instruct model that calls tools), as its one provider, lmstudio
# (docs/adr/0076). The script never loads a model: when the lms CLI shows the model is not loaded,
# it prints the command that loads it with one prediction and a 32k context, because LM Studio
# would load it on the first turn with its own defaults, whose parallel predictions split the
# context (docs/operations/chat.md). Without LM Studio make dev runs without the chat.
CHAT_MODEL=${COWORK_DEV_CHAT_MODEL:-qwen/qwen3-30b-a3b-2507}
chat_env=()
chat_load=""
if curl -sf -m 2 http://localhost:1234/v1/models 2>/dev/null | grep -qF "\"$CHAT_MODEL\""; then
	chat_env=(COWORK_CHAT_PROVIDERS=lmstudio "COWORK_CHAT_LMSTUDIO_NAME=LM Studio" COWORK_CHAT_LMSTUDIO_KIND=openai
		COWORK_CHAT_LMSTUDIO_URL=http://localhost:1234/v1 COWORK_CHAT_LMSTUDIO_MODEL="$CHAT_MODEL")
	chat_note="the chat at the right edge talks to LM Studio's $CHAT_MODEL"
	if command -v lms >/dev/null 2>&1 && ! grep -qF "$CHAT_MODEL" <<<"$(lms ps 2>/dev/null || true)"; then
		chat_load="LM Studio has not loaded $CHAT_MODEL; load it before the first turn (docs/operations/chat.md): lms load $CHAT_MODEL --context-length 32768 --parallel 1"
	fi
else
	chat_note="no chat: LM Studio does not answer on :1234 with $CHAT_MODEL (COWORK_DEV_CHAT_MODEL names another)"
fi
# Dex is the identity provider: it has https://localhost:4200/auth/callback (COWORK_BASE_URL +
# /auth/callback) registered as a redirect URI, members of cowork-users pass the gate, members of
# cowork-admins are global administrators (docs/adr/0030 D1), and offline_access brings the
# refresh token the groups refresh needs (docs/adr/0030 D5).
env ${chat_env[@]+"${chat_env[@]}"} \
	COWORK_DATABASE_URL="$DB_APP" COWORK_DATABASE_OWNER_URL="$DB_OWNER" \
	COWORK_SESSION_KEY="$(cat "$STATE/session-key")" COWORK_LOG_FORMAT=text \
	COWORK_LISTEN_ADDR=127.0.0.1:8080 \
	COWORK_LOCAL_ADMIN_USERNAME="$ADMIN_USER" COWORK_LOCAL_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
	COWORK_BASE_URL="$UI" \
	COWORK_OIDC_ISSUER="http://localhost:$DEX_PORT/dex" COWORK_OIDC_CLIENT_ID=cowork \
	COWORK_OIDC_CLIENT_SECRET="$DEX_CLIENT_SECRET" \
	COWORK_OIDC_SCOPES="openid profile email groups offline_access" \
	COWORK_OIDC_ALLOWED_GROUPS=cowork-users COWORK_ADMIN_GROUP=cowork-admins \
	COWORK_OIDC_DISPLAY_NAME=Dex \
	COWORK_S3_ENDPOINT="http://localhost:$MINIO_PORT" COWORK_S3_BUCKET="$BUCKET" \
	COWORK_S3_ACCESS_KEY_ID="$MINIO_ACCESS_KEY" COWORK_S3_SECRET_ACCESS_KEY="$MINIO_SECRET_KEY" \
	"$STATE/cowork" serve >"$STATE/backend.log" 2>&1 &
backend=$!
trap 'kill "$backend" 2>/dev/null || true; wait "$backend" 2>/dev/null || true' EXIT
for i in $(seq 1 60); do
	curl -sf http://127.0.0.1:8080/readyz >/dev/null && break
	kill -0 "$backend" 2>/dev/null || { cat "$STATE/backend.log"; echo "the backend stopped"; exit 1; }
	sleep 0.5
	[ "$i" -lt 60 ] || { cat "$STATE/backend.log"; echo "the backend did not become ready"; exit 1; }
done

step "the group mapping team-red -> member and demo data in the team dev (the projects only when it has none yet)"
COWORK_DEV_ADMIN="$ADMIN_USER" COWORK_DEV_ADMIN_PASSWORD="$ADMIN_PASSWORD" COWORK_BASE_URL="$UI" \
	python3 hack/dev_demo.py http://127.0.0.1:8080 "$token" dev

step "the UI on $UI; Ctrl-C stops everything. Two ways in, with development-only credentials:"
echo "    the form: $ADMIN_USER with the password $ADMIN_PASSWORD"
echo "    Sign in with Dex: ada@example.com (administrator group), bob@example.com (team-red, a member of dev),"
echo "    cyd@example.com (in no mapped group) or dan@example.com (outside the gate), each with the password $DEX_PASSWORD"
echo "    $chat_note"
[ -z "$chat_load" ] || echo "    $chat_load"
COWORK_DEV_BACKEND=http://127.0.0.1:8080 make -s frontend-serve NG_SERVE_FLAGS=--ssl
