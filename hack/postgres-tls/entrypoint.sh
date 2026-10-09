#!/usr/bin/env bash
# The entrypoint of the PostgreSQL that serves TLS under a private authority (make postgres-tls-up,
# docs/adr/0058 D3), the server the integration tier proves COWORK_DATABASE_CA against. At the
# container's first start it makes an authority and a server certificate the authority issues for
# localhost and 127.0.0.1, then starts the image's own entrypoint with TLS on; a start after that
# finds them. The server's certificate is valid 397 days, as platform verifiers want a server's
# certificate to be; a new container makes a new one. The authority's certificate is what
# make postgres-tls-up copies out; its key is gone once the certificate is issued, and the server's
# key never leaves the container. Every key here is a development value, made anew with the
# container (docs/developer/development-credentials.md).
set -euo pipefail

dir=/etc/postgresql-tls
if [ ! -f "$dir/server.key" ]; then
	mkdir -p "$dir"
	cd "$dir"
	openssl req -x509 -new -noenc -newkey ec -pkeyopt ec_paramgen_curve:P-256 -days 3650 \
		-subj "/CN=cowork test database authority" -keyout ca.key -out ca.crt 2>/dev/null
	openssl req -new -noenc -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
		-subj "/CN=localhost" -keyout server.key -out server.csr 2>/dev/null
	printf '%s\n' 'subjectAltName=DNS:localhost,IP:127.0.0.1' 'basicConstraints=CA:FALSE' \
		'keyUsage=digitalSignature' 'extendedKeyUsage=serverAuth' >server.ext
	openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -days 397 \
		-extfile server.ext -out server.crt 2>/dev/null
	rm -f ca.key ca.srl server.csr server.ext
	chown postgres:postgres server.key server.crt
	chmod 600 server.key
fi

exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file="$dir/server.crt" -c ssl_key_file="$dir/server.key"
