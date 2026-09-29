#!/usr/bin/env bash
set -euo pipefail

newest_dump=$(ls -1t /opt/balce/backups/daily/balce-*.dump 2>/dev/null | head -1)
if [[ -z "$newest_dump" ]]; then
  echo "no backup found in /opt/balce/backups/daily" >&2
  exit 1
fi

run_psql() {
  docker exec -i balce-postgres psql -U postgres -v ON_ERROR_STOP=1 -At "$@"
}

count_rows() {
  run_psql -d "$1" -c "SELECT format('%s %s', table_name, (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM public.%I', table_name), false, true, '')))[1]::text) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name"
}

run_psql -d postgres -c "DROP DATABASE IF EXISTS balce_restore_check WITH (FORCE)" >/dev/null
run_psql -d postgres -c "CREATE DATABASE balce_restore_check" >/dev/null
docker exec -i balce-postgres pg_restore -U postgres -d balce_restore_check --no-owner < "$newest_dump"

live_counts=$(count_rows balce)
restored_counts=$(count_rows balce_restore_check)
run_psql -d postgres -c "DROP DATABASE balce_restore_check WITH (FORCE)" >/dev/null

echo "restored from: $newest_dump"
if [[ "$live_counts" == "$restored_counts" ]]; then
  echo "row counts match ($(printf '%s\n' "$live_counts" | grep -c . || true) tables)"
else
  echo "row counts differ (expected if writes happened after the backup):"
  diff <(printf '%s\n' "$live_counts") <(printf '%s\n' "$restored_counts") || true
fi
