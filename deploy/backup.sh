#!/usr/bin/env bash
set -euo pipefail
umask 077

backup_root=/opt/balce/backups
timestamp=$(date -u +%Y%m%d-%H%M%S)
daily_file="$backup_root/daily/balce-$timestamp.dump"

docker exec balce-postgres pg_dump -U postgres -d balce -Fc > "$daily_file.partial"
mv "$daily_file.partial" "$daily_file"

if [[ "$(date -u +%u)" == "7" ]]; then
  cp "$daily_file" "$backup_root/weekly/"
fi

find "$backup_root/daily" -name 'balce-*.dump' -mtime +7 -delete
find "$backup_root/weekly" -name 'balce-*.dump' -mtime +28 -delete
find "$backup_root" -name '*.partial' -mmin +60 -delete

echo "$(date -u +%FT%TZ) backup ok $(du -h "$daily_file" | cut -f1) $daily_file"
