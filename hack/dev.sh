#!/usr/bin/env bash
# make dev: the whole development stack in one terminal, to watch the UI while it is built.
#
# PostgreSQL (make postgres-up) and MinIO (make minio-up) in containers, the backend built from
# source on :8080 with the local administrator `dev` (docs/adr/0032), demo data in the tenant
# `dev` on the first run, and ng serve on https://localhost:4200 (a self-signed certificate the
# browser asks about once), which reloads the page on every
# saved change. The browser logs in like on an installation: as `dev` with the development-only
# password below (docs/adr/0038 D2, D4). Ctrl-C stops the backend and the dev server; the
# containers keep their data (make postgres-down minio-down removes it). The backend log is in
# .dev/backend.log.
set -euo pipefail
cd "$(dirname "$0")/.."

POSTGRES_PORT=${POSTGRES_PORT:-5432}
MINIO_PORT=${MINIO_PORT:-9000}
MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY:-cowork}
MINIO_SECRET_KEY=${MINIO_SECRET_KEY:-cowork-secret}
BUCKET=cowork-dev
STATE=.dev
# Development-only credentials (docs/adr/0038 D4): meaningless outside localhost.
ADMIN_USER=${COWORK_DEV_ADMIN:-dev}
ADMIN_PASSWORD=${COWORK_DEV_ADMIN_PASSWORD:-dev-only-cowork}
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

step "PostgreSQL and MinIO"
make -s postgres-up POSTGRES_PORT="$POSTGRES_PORT"
make -s minio-up MINIO_PORT="$MINIO_PORT"
code=$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" \
	--user "$MINIO_ACCESS_KEY:$MINIO_SECRET_KEY" -X PUT "http://localhost:$MINIO_PORT/$BUCKET")
case "$code" in 200 | 409) ;; *) echo "creating the bucket $BUCKET answered $code" && exit 1 ;; esac

step "the backend, built from source"
make -s migrate POSTGRES_PORT="$POSTGRES_PORT"
(cd backend && go build -o "../$STATE/cowork" ./cmd/cowork)

step "the person dev with the tenant dev, a second person, and a token for the demo data"
seed() { (cd backend && COWORK_DEV_SEED_DATABASE_URL="$DB_ADMIN" go run ./test/devseed "$@"); }
token=$(seed -agent=false | awk '/^token:/ {print $2}')
[ -n "$token" ] || { echo "make dev-seed printed no token"; exit 1; }
(umask 077 && printf '%s\n' "$token" >"$STATE/token")
seed -username sam -agent=false >/dev/null

# The server key stays the same across restarts: list cursors and the login throttle's address
# hashes outlive a restart (sessions are database rows and outlive it anyway).
[ -s "$STATE/session-key" ] || (umask 077 && openssl rand -base64 32 >"$STATE/session-key")
COWORK_DATABASE_URL="$DB_APP" COWORK_DATABASE_OWNER_URL="$DB_OWNER" \
	COWORK_SESSION_KEY="$(cat "$STATE/session-key")" COWORK_LOG_FORMAT=text \
	COWORK_LOCAL_ADMIN_USERNAME="$ADMIN_USER" COWORK_LOCAL_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
	COWORK_BASE_URL="$UI" \
	COWORK_S3_ENDPOINT="http://localhost:$MINIO_PORT" COWORK_S3_BUCKET="$BUCKET" \
	COWORK_S3_ACCESS_KEY_ID="$MINIO_ACCESS_KEY" COWORK_S3_SECRET_ACCESS_KEY="$MINIO_SECRET_KEY" \
	"$STATE/cowork" serve >"$STATE/backend.log" 2>&1 &
backend=$!
trap 'kill "$backend" 2>/dev/null || true; wait "$backend" 2>/dev/null || true' EXIT
for i in $(seq 1 60); do
	curl -sf http://localhost:8080/readyz >/dev/null && break
	kill -0 "$backend" 2>/dev/null || { cat "$STATE/backend.log"; echo "the backend stopped"; exit 1; }
	sleep 0.5
	[ "$i" -lt 60 ] || { cat "$STATE/backend.log"; echo "the backend did not become ready"; exit 1; }
done

step "demo data in the tenant dev (only when it has no project yet)"
python3 hack/dev_demo.py http://localhost:8080 "$token" dev

step "the UI on $UI — sign in as $ADMIN_USER with the password $ADMIN_PASSWORD (development only); Ctrl-C stops everything"
make -s frontend-serve NG_SERVE_FLAGS=--ssl
