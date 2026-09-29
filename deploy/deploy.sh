#!/usr/bin/env bash
set -euo pipefail

server_address=${BALCE_SERVER:-root@140.99.254.193}
stack_directory=/opt/balce
release_tag=$(date -u +%Y%m%d-%H%M%S)
is_dry_run=false
if [[ "${1:-}" == "--dry-run" ]]; then
  is_dry_run=true
fi

script_directory=$(cd "$(dirname "$0")" && pwd)
backend_directory=$(dirname "$script_directory")
build_directory="$script_directory/build"

step() {
  echo "==> $*"
}

run() {
  if [[ "$is_dry_run" == true ]]; then
    echo "    [dry-run] $*"
    return 0
  fi
  "$@"
}

step "build linux/amd64 binary"
run mkdir -p "$build_directory"
run env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -C "$backend_directory" -trimpath -ldflags "-s -w" -o "$build_directory/balce-api" ./cmd/server

step "check the api service exists on the server"
run ssh "$server_address" "docker compose -f $stack_directory/docker-compose.prod.yml config --services | grep -qx api"

step "upload binary and Dockerfile"
run ssh "$server_address" "install -d -m 700 $stack_directory/api"
run scp "$build_directory/balce-api" "$script_directory/Dockerfile.api" "$server_address:$stack_directory/api/"

step "back up the database before migrating"
run ssh "$server_address" "$stack_directory/backup.sh"

step "build image balce-api:$release_tag and start it"
release_script="set -e
cd $stack_directory
docker build -q -t balce-api:$release_tag -f api/Dockerfile.api api
current_tag=\$(grep '^BALCE_API_TAG=' .env.prod | cut -d= -f2 || true)
sed -i '/^BALCE_API_TAG=/d;/^PREVIOUS_BALCE_API_TAG=/d' .env.prod
echo PREVIOUS_BALCE_API_TAG=\$current_tag >> .env.prod
echo BALCE_API_TAG=$release_tag >> .env.prod
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d api"
run ssh "$server_address" "$release_script"

step "wait for /health"
run ssh "$server_address" "for attempt in \$(seq 1 30); do curl -fsS http://127.0.0.1:8080/health >/dev/null && exit 0; sleep 2; done; exit 1" || {
  step "unhealthy: rolling back to the previous tag"
  run ssh "$server_address" "cd $stack_directory && previous=\$(grep '^PREVIOUS_BALCE_API_TAG=' .env.prod | cut -d= -f2) && [ -n \"\$previous\" ] && sed -i \"s/^BALCE_API_TAG=.*/BALCE_API_TAG=\$previous/\" .env.prod && docker compose --env-file .env.prod -f docker-compose.prod.yml up -d api"
  exit 1
}

step "deployed balce-api:$release_tag"
