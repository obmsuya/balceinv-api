#!/usr/bin/env bash
set -euo pipefail

server_address=${BALCE_SERVER:-root@140.99.254.193}
local_directory=${BALCE_BACKUP_DIR:-$HOME/BalceBackups}

mkdir -p "$local_directory"
rsync -az "$server_address:/opt/balce/backups/" "$local_directory/"
echo "backups copied to $local_directory"
