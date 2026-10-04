#!/usr/bin/env bash
# The phase-2 verification, run by hand and not a required job: both built
# images, read-only, behind the Ingress stand-in of hack/ingress/default.conf
# (the chart's path routing, docs/adr/0001 D3), against PostgreSQL 18 (make
# postgres-up) and the S3 test server (make minio-up), driven through the
# stand-in by an agent whose token `make dev-seed` printed.
# hack/verify_phase_2.py makes the API calls and asserts the audit record; the
# first failed assertion stops it.
set -euo pipefail

POSTGRES_CONTAINER=${POSTGRES_CONTAINER:-cowork-postgres}
POSTGRES_PORT=${POSTGRES_PORT:-5432}
MINIO_PORT=${MINIO_PORT:-9000}
MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY:-cowork}
MINIO_SECRET_KEY=${MINIO_SECRET_KEY:-cowork-secret}
BACKEND_IMG=${BACKEND_IMG:-guidedtraffic/cowork-backend:latest}
FRONTEND_IMG=${FRONTEND_IMG:-guidedtraffic/cowork-frontend:latest}
INGRESS_IMAGE=${INGRESS_IMAGE:-nginxinc/nginx-unprivileged:1.31-alpine}
PORT=${VERIFY_PORT:-18090}
DB=cowork_verify
BUCKET=cowork-verify
NET=cowork-verify
DBHOST=host.docker.internal

cleanup() {
	docker rm -f cowork-verify-ingress cowork-verify-frontend cowork-verify-backend >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
step() { echo "==> $*"; }

step "a fresh database $DB, owned by cowork_owner"
docker exec "$POSTGRES_CONTAINER" psql -U postgres -q -v ON_ERROR_STOP=1 \
	-c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB OWNER cowork_owner"

step "the bucket $BUCKET"
code=$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" \
	--user "$MINIO_ACCESS_KEY:$MINIO_SECRET_KEY" -X PUT "http://localhost:$MINIO_PORT/$BUCKET")
case "$code" in 200|409) ;; *) echo "creating the bucket answered $code"; exit 1 ;; esac

step "both images on one network, read-only, behind the Ingress stand-in"
cleanup
docker network create "$NET" >/dev/null
docker run -d --name cowork-verify-backend --network "$NET" --network-alias backend --add-host="$DBHOST:host-gateway" \
	--read-only --user 65532:65532 \
	-e COWORK_DATABASE_URL="postgres://cowork_app:cowork_app@$DBHOST:$POSTGRES_PORT/$DB?sslmode=disable" \
	-e COWORK_DATABASE_OWNER_URL="postgres://cowork_owner:cowork_owner@$DBHOST:$POSTGRES_PORT/$DB?sslmode=disable" \
	-e COWORK_SESSION_KEY="$(openssl rand -base64 32)" \
	-e COWORK_S3_ENDPOINT="http://$DBHOST:$MINIO_PORT" -e COWORK_S3_BUCKET="$BUCKET" \
	-e COWORK_S3_ACCESS_KEY_ID="$MINIO_ACCESS_KEY" -e COWORK_S3_SECRET_ACCESS_KEY="$MINIO_SECRET_KEY" \
	-e COWORK_LOG_FORMAT=text "$BACKEND_IMG" serve >/dev/null
docker run -d --name cowork-verify-frontend --network "$NET" --network-alias frontend --read-only \
	--tmpfs /tmp:uid=101,gid=101 --user 101:101 "$FRONTEND_IMG" >/dev/null
# The stand-in's configuration is copied in, not mounted, as make dex-up does
# with Dex's: a Docker daemon of a CI runner need not see this checkout.
docker create --name cowork-verify-ingress --network "$NET" -p "127.0.0.1:$PORT:8080" "$INGRESS_IMAGE" >/dev/null
docker cp "$(dirname "$0")/ingress/default.conf" cowork-verify-ingress:/etc/nginx/conf.d/default.conf
docker start cowork-verify-ingress >/dev/null
for i in $(seq 1 60); do
	curl -sf "http://localhost:$PORT/api/v1/version" >/dev/null && break
	sleep 1
	[ "$i" -lt 60 ] || { docker logs cowork-verify-backend; echo "the backend did not answer through the Ingress stand-in"; exit 1; }
done

step "a token from make dev-seed"
# make dev-seed migrates first, and without these the migration would run on
# the development database of make dev; the backend container migrated $DB.
token=$(COWORK_DATABASE_URL="postgres://cowork_app:cowork_app@localhost:$POSTGRES_PORT/$DB?sslmode=disable" \
	COWORK_DATABASE_OWNER_URL="postgres://cowork_owner:cowork_owner@localhost:$POSTGRES_PORT/$DB?sslmode=disable" \
	COWORK_DEV_SEED_DATABASE_URL="postgres://postgres:postgres@localhost:$POSTGRES_PORT/$DB?sslmode=disable" \
	make -s dev-seed | awk '/^token:/ {print $2}')
[ -n "$token" ] || { echo "make dev-seed printed no token"; exit 1; }

step "the API through the Ingress stand-in, as an agent"
python3 "$(dirname "$0")/verify_phase_2.py" "http://localhost:$PORT" "$token" dev
