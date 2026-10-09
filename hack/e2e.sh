#!/usr/bin/env bash
# The end-to-end tier (docs/adr/0056): both built images behind the Ingress stand-in of
# hack/ingress/default.conf, terminating TLS with a certificate made for this run, against a
# PostgreSQL, a Silo and a Dex of its own, and the Playwright suite of frontend/e2e/ in a browser on
# this machine. `make e2e` runs `up`, the suite and `down`; `make e2e-up` and `make e2e-down` keep the
# stack between runs of the suite while it is written.
#
# Every container and the network are this script's own (E2E_NAME, by default cowork-e2e): it never
# touches the containers of make dev-up, and `down` removes what `up` made. Nothing is kept: the
# database, the bucket and Dex's state go with their containers.
# Only two ports are published, on the loopback address: the stand-in's HTTPS port (E2E_PORT) and
# Dex's (E2E_DEX_PORT). PostgreSQL and Silo are reachable on the run's network only. Silo, the
# maintained MinIO fork, keeps MinIO's interface, so its container and network alias keep the name
# minio.
#
# The identity provider must be one URL for the browser and for the backend, and the backend takes
# plain http only on a loopback host (internal/config/oidc.go). So Dex's container holds the network
# namespace that the backend and the stand-in join (--network container:), as containers of one pod
# share localhost: http://localhost:E2E_DEX_PORT/dex is Dex inside it and, through the published
# port, in the browser; https://localhost:E2E_PORT is the stand-in in both.
#
# Every credential here is development-only and public (docs/developer/development-credentials.md).
set -euo pipefail
cd "$(dirname "$0")/.."

NAME=${E2E_NAME:-cowork-e2e}
PORT=${E2E_PORT:-18443}
DEX_PORT=${E2E_DEX_PORT:-5557}
BACKEND_IMG=${BACKEND_IMG:-guidedtraffic/cowork-backend:latest}
FRONTEND_IMG=${FRONTEND_IMG:-guidedtraffic/cowork-frontend:latest}
INGRESS_IMAGE=${INGRESS_IMAGE:-nginxinc/nginx-unprivileged:1.31-alpine}
POSTGRES_IMAGE=${POSTGRES_IMAGE:-postgres:18}
MINIO_IMAGE=${MINIO_IMAGE:-docker.io/pgsty/silo:RELEASE.2026-09-16T00-00-00Z@sha256:635197cb9f36d01bee221d34d1c7d7960f6a95c48b0b6c01d99cd13bdae51a46}
DEX_IMAGE=${DEX_IMAGE:-ghcr.io/dexidp/dex:v2.45.1}
BIND=${CONTAINER_BIND:-127.0.0.1}
ADMIN_USER=${COWORK_E2E_ADMIN:-e2e-admin}
ADMIN_PASSWORD=${COWORK_E2E_ADMIN_PASSWORD:-e2e-only-cowork}
DEX_CLIENT_SECRET=cowork-dev-dex-secret
MINIO_KEY=cowork-e2e
MINIO_SECRET=cowork-e2e-secret
BUCKET=cowork-e2e
DB=cowork_e2e
BASE_URL="https://localhost:$PORT"
ISSUER="http://localhost:$DEX_PORT/dex"
RESULTS=frontend/e2e/test-results
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

step() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }
containers() { printf '%s\n' "$NAME-ingress" "$NAME-backend" "$NAME-frontend" "$NAME-dex" "$NAME-minio" "$NAME-postgres"; }

down() {
	# The joiners first: a container cannot outlive the namespace it joined.
	containers | xargs docker rm -f -v >/dev/null 2>&1 || true
	docker network rm "$NAME" >/dev/null 2>&1 || true
}

# The images must be of one commit (docs/adr/0056 D1): their revision labels are compared, as
# make docker-build and the container-scan job write them.
same_commit() {
	local back front
	back=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$BACKEND_IMG") ||
		{ echo "no image $BACKEND_IMG: run make docker-build (or pass BACKEND_IMG=)"; exit 1; }
	front=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$FRONTEND_IMG") ||
		{ echo "no image $FRONTEND_IMG: run make docker-build (or pass FRONTEND_IMG=)"; exit 1; }
	if [ "$back" != "$front" ]; then
		echo "the images are of two commits: $BACKEND_IMG of $back, $FRONTEND_IMG of $front; build both with make docker-build"
		exit 1
	fi
	echo "both images of commit $back"
}

wait_for() {
	local what=$1 container=$2
	shift 2
	for i in $(seq 1 60); do
		"$@" >/dev/null 2>&1 && return 0
		sleep 1
		if [ "$i" -eq 60 ]; then
			docker logs --tail 40 "$container" || true
			echo "$what did not become ready"
			exit 1
		fi
	done
}

up() {
	same_commit
	down
	local tmp=$TMP
	docker network create "$NAME" >/dev/null

	step "PostgreSQL 18: the database $DB, owned by cowork_owner, served as cowork_app"
	docker run -d --name "$NAME-postgres" --network "$NAME" --network-alias postgres \
		-e POSTGRES_PASSWORD=postgres "$POSTGRES_IMAGE" >/dev/null
	wait_for PostgreSQL "$NAME-postgres" docker exec "$NAME-postgres" pg_isready -U postgres -h 127.0.0.1
	docker exec "$NAME-postgres" psql -U postgres -q -v ON_ERROR_STOP=1 \
		-c "CREATE ROLE cowork_owner LOGIN PASSWORD 'cowork_owner'" \
		-c "CREATE ROLE cowork_app LOGIN PASSWORD 'cowork_app'" \
		-c "CREATE DATABASE $DB OWNER cowork_owner"

	step "Silo: the bucket $BUCKET"
	# Published on a port Docker chooses, for the one request that makes the bucket; the backend
	# reaches it on the run's network. The server never makes its bucket (docs/adr/0058 D5).
	docker run -d --name "$NAME-minio" --network "$NAME" --network-alias minio -p "$BIND::9000" \
		-e MINIO_ROOT_USER="$MINIO_KEY" -e MINIO_ROOT_PASSWORD="$MINIO_SECRET" "$MINIO_IMAGE" server /data >/dev/null
	local s3
	s3=$(docker port "$NAME-minio" 9000/tcp | head -1)
	wait_for Silo "$NAME-minio" curl -sf "http://$s3/minio/health/live"
	local code
	code=$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" \
		--user "$MINIO_KEY:$MINIO_SECRET" -X PUT "http://$s3/$BUCKET")
	case "$code" in 200 | 409) ;; *) echo "creating the bucket $BUCKET answered $code" && exit 1 ;; esac

	step "Dex: the issuer $ISSUER, holding the network namespace of the backend and the stand-in"
	# hack/dex/config.yaml with the issuer, the port Dex listens on and the suite's redirect URI
	# moved to this run's ports; each substitution must hit, or the file changed under this script.
	sed -e "s#http://localhost:5556/dex#$ISSUER#" -e "s#0.0.0.0:5556#0.0.0.0:$DEX_PORT#" \
		-e "s#https://localhost:18443/auth/callback#$BASE_URL/auth/callback#" hack/dex/config.yaml >"$tmp/dex.yaml"
	for want in "issuer: $ISSUER" "http: 0.0.0.0:$DEX_PORT" "- $BASE_URL/auth/callback"; do
		grep -qF -- "$want" "$tmp/dex.yaml" || { echo "hack/dex/config.yaml: could not set '$want'"; exit 1; }
	done
	chmod 644 "$tmp/dex.yaml"
	docker create --name "$NAME-dex" --network "$NAME" --network-alias backend \
		-p "$BIND:$DEX_PORT:$DEX_PORT" -p "$BIND:$PORT:$PORT" \
		"$DEX_IMAGE" dex serve /etc/dex/cowork.yaml >/dev/null
	docker cp "$tmp/dex.yaml" "$NAME-dex:/etc/dex/cowork.yaml"
	docker start "$NAME-dex" >/dev/null
	wait_for Dex "$NAME-dex" curl -sf "$ISSUER/.well-known/openid-configuration"

	step "the backend ($BACKEND_IMG), read-only, in Dex's network namespace"
	# The per-address login throttle is off: every browser of the run reaches the backend through
	# the one stand-in, so all of them are one address to it — as the integration tier has it.
	docker run -d --name "$NAME-backend" --network "container:$NAME-dex" --read-only --user 65532:65532 \
		-e COWORK_DATABASE_URL="postgres://cowork_app:cowork_app@postgres:5432/$DB?sslmode=disable" \
		-e COWORK_DATABASE_OWNER_URL="postgres://cowork_owner:cowork_owner@postgres:5432/$DB?sslmode=disable" \
		-e COWORK_SESSION_KEY="$(openssl rand -base64 32)" -e COWORK_LOG_FORMAT=text \
		-e COWORK_BASE_URL="$BASE_URL" \
		-e COWORK_LOCAL_ADMIN_USERNAME="$ADMIN_USER" -e COWORK_LOCAL_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
		-e COWORK_LOGIN_ADDRESS_LIMIT=0 \
		-e COWORK_OIDC_ISSUER="$ISSUER" -e COWORK_OIDC_CLIENT_ID=cowork \
		-e COWORK_OIDC_CLIENT_SECRET="$DEX_CLIENT_SECRET" \
		-e COWORK_OIDC_SCOPES="openid profile email groups offline_access" \
		-e COWORK_OIDC_ALLOWED_GROUPS=cowork-users -e COWORK_ADMIN_GROUP=cowork-admins \
		-e COWORK_OIDC_DISPLAY_NAME=Dex \
		-e COWORK_S3_ENDPOINT=http://minio:9000 -e COWORK_S3_BUCKET="$BUCKET" \
		-e COWORK_S3_ACCESS_KEY_ID="$MINIO_KEY" -e COWORK_S3_SECRET_ACCESS_KEY="$MINIO_SECRET" \
		"$BACKEND_IMG" serve >/dev/null

	step "the frontend ($FRONTEND_IMG), read-only, as the chart runs it"
	docker run -d --name "$NAME-frontend" --network "$NAME" --network-alias frontend --read-only \
		--tmpfs /tmp:uid=101,gid=101 --user 101:101 "$FRONTEND_IMG" >/dev/null

	step "the Ingress stand-in on $BASE_URL, terminating TLS with a certificate made for this run"
	# hack/ingress/default.conf as it is, but listening with TLS on the published port; the
	# certificate and its key exist for this run only and are never stored.
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 2 \
		-subj /CN=localhost -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
		-keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
	chmod 644 "$tmp/key.pem" "$tmp/cert.pem"
	sed "s#^    listen       8080;#    listen       $PORT ssl;#" hack/ingress/default.conf >"$tmp/default.conf"
	grep -qF "listen       $PORT ssl;" "$tmp/default.conf" || { echo "hack/ingress/default.conf: no 'listen 8080;' to turn into TLS"; exit 1; }
	printf 'ssl_certificate     /etc/nginx/tls/cert.pem;\nssl_certificate_key /etc/nginx/tls/key.pem;\n' >"$tmp/00-tls.conf"
	mkdir "$tmp/tls" && cp "$tmp/cert.pem" "$tmp/key.pem" "$tmp/tls/"
	docker create --name "$NAME-ingress" --network "container:$NAME-dex" "$INGRESS_IMAGE" >/dev/null
	docker cp "$tmp/default.conf" "$NAME-ingress:/etc/nginx/conf.d/default.conf"
	docker cp "$tmp/00-tls.conf" "$NAME-ingress:/etc/nginx/conf.d/00-tls.conf"
	docker cp "$tmp/tls" "$NAME-ingress:/etc/nginx/tls"
	docker start "$NAME-ingress" >/dev/null
	wait_for "the backend through the stand-in" "$NAME-backend" curl -sfk "$BASE_URL/api/v1/version"
	wait_for "the frontend through the stand-in" "$NAME-frontend" curl -sfk "$BASE_URL/healthz"
	echo "the stack is up: $BASE_URL (the local administrator $ADMIN_USER), Dex at $ISSUER"
}

# The containers' logs beside the suite's results, for a failed run.
keep_logs() {
	mkdir -p "$RESULTS/containers"
	local c
	for c in $(containers); do
		docker logs "$c" >"$RESULTS/containers/${c#"$NAME"-}.log" 2>&1 || true
	done
	echo "the containers' logs are in $RESULTS/containers/"
}

suite() {
	step "the suite against $BASE_URL"
	(cd frontend && COWORK_BASE_URL="$BASE_URL" COWORK_E2E_ADMIN="$ADMIN_USER" \
		COWORK_E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD" npx playwright test -c e2e "$@")
}

case "${1:-run}" in
up) up ;;
down) down ;;
run)
	shift || true
	trap 'rm -rf "$TMP"; down' EXIT
	up
	if ! suite "$@"; then
		keep_logs
		exit 1
	fi
	;;
*)
	echo "usage: hack/e2e.sh [run [playwright arguments] | up | down]"
	exit 2
	;;
esac
