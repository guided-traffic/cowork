#!/bin/sh
# AN EXAMPLE TO COPY AND ADAPT, NOT A SUPPORTED DEPLOYMENT (docs/adr/0058 D1, D2).
# Written against mcli RELEASE.2026-09-16T00-00-00Z, the client that PGSTY Silo
# RELEASE.2026-09-16T00-00-00Z carries in its image (docker.io/pgsty/silo) and
# publishes as docker.io/pgsty/mc; it reads MinIO's mc commands. `make
# examples-lint` checks only that `sh -n` reads it. Run on 2026-10-09, with
# another namespace and a stand-in for kubectl, against the Silo of
# `make minio-up`: the key it made put, read, listed and deleted objects in its
# bucket and was refused listing and writing another bucket, making a bucket
# and the administration.
#
# What it makes, run by the store's administrator, for the Silo of
# silo-values.yaml or another one — against a MinIO it was not tried: the
# bucket cowork, the policy cowork-attachments that reaches that bucket and
# nothing else, a user whose access key has that policy alone
# (docs/adr/0058 D5), and the Secret cowork-storage with that key under the keys
# the chart reads by default, accessKeyId and secretAccessKey. cowork never sees
# the administrator's keys; the chart's values are storage.existingSecret:
# cowork-storage, storage.endpoint (SILO_URL below) and storage.bucket: cowork.
#
# The administrator's credentials come from the environment, never from this
# file:
#   SILO_URL=https://silo.example.com SILO_ROOT_USER=... SILO_ROOT_PASSWORD=... \
#     NAMESPACE=cowork sh silo-bucket.sh
# mcli trusts the system's authorities and those in ~/.mcli/certs/CAs, where a
# private one goes. Every name below is an example.
set -eu

ALIAS=cowork-admin                 # the administrator's alias, local to this machine
BUCKET=cowork
POLICY=cowork-attachments
ACCESS_KEY=cowork-app              # the user cowork logs in as
SECRET_KEY=$(openssl rand -hex 20) # 40 characters, within the store's 8 to 40

# mcli keeps an alias's keys in its configuration file: the trap removes the
# alias and the policy file however the script ends.
policy_file=$(mktemp)
trap 'rm -f "$policy_file"; mcli alias remove "$ALIAS" >/dev/null 2>&1 || true' EXIT
mcli alias set "$ALIAS" "$SILO_URL" "$SILO_ROOT_USER" "$SILO_ROOT_PASSWORD"
mcli mb --ignore-existing "$ALIAS/$BUCKET"

# The backend writes, reads and deletes objects under <tenant-id>/<attachment-id>,
# and its daily consistency check lists the bucket's keys under each tenant's
# prefix (docs/operations/installation.md#object-storage); it never creates or
# deletes a bucket.
cat >"$policy_file" <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::$BUCKET/*"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::$BUCKET"]
    }
  ]
}
EOF
mcli admin policy create "$ALIAS" "$POLICY" "$policy_file"

mcli admin user add "$ALIAS" "$ACCESS_KEY" "$SECRET_KEY"
mcli admin policy attach "$ALIAS" "$POLICY" --user "$ACCESS_KEY"

kubectl -n "$NAMESPACE" create secret generic cowork-storage \
  --from-literal=accessKeyId="$ACCESS_KEY" --from-literal=secretAccessKey="$SECRET_KEY"
