#!/bin/sh
# AN EXAMPLE TO COPY AND ADAPT, NOT A SUPPORTED DEPLOYMENT (docs/adr/0058 D1, D2).
# Written against the MinIO client mc RELEASE.2025-08-13T08-35-41Z, its last
# release. `make examples-lint` checks only that `sh -n` reads it. Run once, on
# 2026-10-06, with other names and a stand-in for kubectl, against the MinIO of
# `make minio-up` — Chainguard's build, version 2026-09-22T19-25-18Z: the key it
# made put, read and deleted objects in its bucket and was refused listing the
# bucket, another bucket and the administration. The repositories of the MinIO
# server and of mc are archived on GitHub, and neither image can be pulled from
# Docker Hub or quay.io any more (both checked 2026-10-06): no fix follows from
# MinIO.
#
# What it makes, run by the store's administrator, for an existing MinIO or
# the Tenant of minio-tenant.yaml: the bucket cowork, the policy
# cowork-attachments that reaches the objects of that bucket and nothing else,
# a user whose access key has that policy alone (docs/adr/0058 D5), and the
# Secret cowork-storage with that key under the keys the chart reads by
# default, accessKeyId and secretAccessKey. cowork never sees the
# administrator's keys; the chart's values are storage.existingSecret:
# cowork-storage, storage.endpoint (MINIO_URL below) and storage.bucket: cowork.
#
# The administrator's credentials come from the environment, never from this
# file:
#   MINIO_URL=https://minio.example.com MINIO_ROOT_USER=... MINIO_ROOT_PASSWORD=... \
#     NAMESPACE=cowork sh minio-bucket.sh
# Every name below is an example.
set -eu

ALIAS=cowork-admin                 # the administrator's alias, local to this machine
BUCKET=cowork
POLICY=cowork-attachments
ACCESS_KEY=cowork-app              # the user cowork logs in as
SECRET_KEY=$(openssl rand -hex 20) # 40 characters, within MinIO's 8 to 40

# mc keeps an alias's keys in its configuration file: the trap removes the
# alias and the policy file however the script ends.
policy_file=$(mktemp)
trap 'rm -f "$policy_file"; mc alias remove "$ALIAS" >/dev/null 2>&1 || true' EXIT
mc alias set "$ALIAS" "$MINIO_URL" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
mc mb --ignore-existing "$ALIAS/$BUCKET"

# The backend writes, reads and deletes objects under <tenant-id>/<attachment-id>;
# it never lists, creates or deletes a bucket.
cat >"$policy_file" <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::$BUCKET/*"]
    }
  ]
}
EOF
mc admin policy create "$ALIAS" "$POLICY" "$policy_file"

mc admin user add "$ALIAS" "$ACCESS_KEY" "$SECRET_KEY"
mc admin policy attach "$ALIAS" "$POLICY" --user "$ACCESS_KEY"

kubectl -n "$NAMESPACE" create secret generic cowork-storage \
  --from-literal=accessKeyId="$ACCESS_KEY" --from-literal=secretAccessKey="$SECRET_KEY"
