#!/usr/bin/env bash
set -euo pipefail
umask 077

env_file=/opt/balce/.env.prod
bucket_name=balce-media
key_name=balce-api

run_garage() {
  docker exec balce-garage /garage "$@"
}

if run_garage status | grep -q "NO ROLE ASSIGNED"; then
  node_id=$(run_garage node id -q | cut -d@ -f1)
  run_garage layout assign -z dc1 -c 10G "$node_id" >/dev/null
  run_garage layout apply --version 1 >/dev/null
  echo "layout applied"
fi

if ! run_garage bucket info "$bucket_name" >/dev/null 2>&1; then
  run_garage bucket create "$bucket_name" >/dev/null
  echo "bucket $bucket_name created"
fi

existing_key_id=$(grep -E "^S3_ACCESS_KEY_ID=" "$env_file" | cut -d= -f2- || true)
if [[ -z "$existing_key_id" ]]; then
  key_output=$(run_garage key create "$key_name")
  new_key_id=$(printf '%s\n' "$key_output" | awk '/^Key ID:/ {print $3}')
  new_secret=$(printf '%s\n' "$key_output" | awk '/^Secret key:/ {print $3}')
  if [[ -z "$new_key_id" || -z "$new_secret" ]]; then
    echo "could not read the new key from garage output; nothing written" >&2
    exit 1
  fi
  sed -i '/^S3_ACCESS_KEY_ID=/d;/^S3_SECRET_ACCESS_KEY=/d' "$env_file"
  printf 'S3_ACCESS_KEY_ID=%s\nS3_SECRET_ACCESS_KEY=%s\n' "$new_key_id" "$new_secret" >> "$env_file"
  run_garage bucket allow --read --write --owner "$bucket_name" --key "$key_name" >/dev/null
  echo "access key $key_name created and stored in $env_file"
fi

run_garage status | sed -n '1,6p'
