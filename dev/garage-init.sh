#!/usr/bin/env bash
set -euo pipefail

compose_file="$(dirname "$0")/compose.yml"

run_garage() {
  docker compose -f "$compose_file" exec -T garage /garage "$@"
}

if run_garage status 2>/dev/null | grep -q "NO ROLE ASSIGNED"; then
  node_id=$(run_garage node id -q 2>/dev/null | cut -d@ -f1)
  run_garage layout assign -z dev -c 1G "$node_id" >/dev/null 2>&1
  run_garage layout apply --version 1 >/dev/null 2>&1
fi

run_garage bucket info balce-test >/dev/null 2>&1 || run_garage bucket create balce-test >/dev/null 2>&1

if ! run_garage key info balce-dev >/dev/null 2>&1; then
  run_garage key create balce-dev >/dev/null 2>&1
  run_garage bucket allow --read --write --owner balce-test --key balce-dev >/dev/null 2>&1
fi

key_details=$(run_garage key info balce-dev --show-secret 2>/dev/null)
echo "export TEST_S3_ENDPOINT=http://127.0.0.1:53900"
echo "export TEST_S3_BUCKET=balce-test"
echo "export TEST_S3_ACCESS_KEY_ID=$(printf '%s\n' "$key_details" | awk '/^Key ID:/ {print $3}')"
echo "export TEST_S3_SECRET_ACCESS_KEY=$(printf '%s\n' "$key_details" | awk '/^Secret key:/ {print $3}')"
