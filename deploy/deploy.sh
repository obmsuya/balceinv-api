#!/usr/bin/env bash
set -euo pipefail

server_address=${BALCE_SERVER:-root@140.99.254.193}
public_url=${BALCE_PUBLIC_URL:-https://api-pos.faltasi.com}
stack_directory=/opt/balce
release_tag=$(date -u +%Y%m%d-%H%M%S)
is_dry_run=false
if [[ "${1:-}" == "--dry-run" ]]; then
  is_dry_run=true
fi

script_directory=$(cd "$(dirname "$0")" && pwd)
backend_directory=$(dirname "$script_directory")
frontend_directory=${BALCE_FRONTEND_DIR:-$(dirname "$backend_directory")/frontend}
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

if [[ ! -f "$frontend_directory/nuxt.config.ts" ]]; then
  echo "frontend not found at $frontend_directory; set BALCE_FRONTEND_DIR" >&2
  exit 1
fi

step "check the server has ALLOWED_ORIGINS in .env.prod"
run ssh "$server_address" "grep -q '^ALLOWED_ORIGINS=https://' $stack_directory/.env.prod"

step "build the web app"
run pnpm --dir "$frontend_directory" install --frozen-lockfile
run env NUXT_PUBLIC_API_BASE= pnpm --dir "$frontend_directory" generate

step "build linux/amd64 server and admin binaries"
run rm -rf "$build_directory"
run mkdir -p "$build_directory/logs"
run touch "$build_directory/logs/.keep"
run env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -C "$backend_directory" -trimpath -ldflags "-s -w" -o "$build_directory/balce-api" ./cmd/server
run env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -C "$backend_directory" -trimpath -ldflags "-s -w" -o "$build_directory/balce-admin" ./cmd/admin
run cp -R "$frontend_directory/.output/public" "$build_directory/web"
run cp "$script_directory/Dockerfile.api" "$build_directory/"

step "upload the compose file and the image context"
run scp "$script_directory/docker-compose.prod.yml" "$server_address:$stack_directory/docker-compose.prod.yml"
run ssh "$server_address" "rm -rf $stack_directory/api && install -d -m 700 $stack_directory/api"
if [[ "$is_dry_run" == true ]]; then
  echo "    [dry-run] tar -C $build_directory . | ssh $server_address tar -x -C $stack_directory/api"
else
  COPYFILE_DISABLE=1 tar -C "$build_directory" -cf - . | ssh "$server_address" "tar -x -C $stack_directory/api"
fi

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

step "wait for /health on the server"
run ssh "$server_address" "for attempt in \$(seq 1 30); do curl -fsS http://127.0.0.1:8080/health >/dev/null && exit 0; sleep 2; done; exit 1" || {
  step "unhealthy: rolling back to the previous tag"
  run ssh "$server_address" "cd $stack_directory && previous=\$(grep '^PREVIOUS_BALCE_API_TAG=' .env.prod | cut -d= -f2) && [ -n \"\$previous\" ] && sed -i \"s/^BALCE_API_TAG=.*/BALCE_API_TAG=\$previous/\" .env.prod && docker compose --env-file .env.prod -f docker-compose.prod.yml up -d api"
  exit 1
}

step "check $public_url through Cloudflare"
run curl -fsS -o /dev/null "$public_url/health"

step "remove images older than the last two releases"
run ssh "$server_address" "docker images balce-api --format '{{.Tag}}' | sort -r | tail -n +3 | xargs -r -I{} docker rmi -f balce-api:{} >/dev/null"

step "deployed balce-api:$release_tag"
