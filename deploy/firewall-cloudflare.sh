#!/usr/bin/env bash
set -euo pipefail

rule_comment=cloudflare

current_ranges=$( { curl -fsS --max-time 20 https://www.cloudflare.com/ips-v4; echo; curl -fsS --max-time 20 https://www.cloudflare.com/ips-v6; } | grep -E '^[0-9a-f:.]+/[0-9]+$' )
range_count=$(printf '%s\n' "$current_ranges" | grep -c . || true)
if (( range_count < 10 )); then
  echo "got only $range_count Cloudflare ranges; firewall left unchanged" >&2
  exit 1
fi

for address_range in $current_ranges; do
  ufw allow proto tcp from "$address_range" to any port 80,443 comment "$rule_comment" >/dev/null
done

ufw delete allow 80/tcp >/dev/null 2>&1 || true
ufw delete allow 443/tcp >/dev/null 2>&1 || true

stale_ranges=$(ufw show added | grep "comment '$rule_comment'" | awk '{print $6}' | grep -vxF -f <(printf '%s\n' "$current_ranges") || true)
for address_range in $stale_ranges; do
  ufw delete allow proto tcp from "$address_range" to any port 80,443 >/dev/null
done

echo "ports 80 and 443 open only to $range_count Cloudflare ranges"
