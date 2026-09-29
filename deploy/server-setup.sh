#!/usr/bin/env bash
set -euo pipefail

if systemctl is-enabled fwupd.service >/dev/null 2>&1 || systemctl is-active fwupd.service >/dev/null 2>&1; then
  systemctl stop fwupd.service fwupd-refresh.timer 2>/dev/null || true
  systemctl mask fwupd.service fwupd-refresh.service fwupd-refresh.timer >/dev/null
  echo "fwupd stopped and masked"
fi

if ! swapon --show | grep -q /swapfile; then
  fallocate -l 1G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  sysctl -q vm.swappiness=10
  echo 'vm.swappiness=10' > /etc/sysctl.d/90-balce-swap.conf
  echo "1G swap enabled"
fi

ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw limit 22/tcp >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw --force enable >/dev/null
echo "ufw active: 22 (rate limited), 80, 443"

install -d -m 700 /opt/balce /opt/balce/backups /opt/balce/backups/daily /opt/balce/backups/weekly
install -d -m 755 /opt/proxy /opt/proxy/sites

cat > /etc/cron.d/balce-backup <<'CRON'
30 2 * * * root /opt/balce/backup.sh >> /opt/balce/backups/backup.log 2>&1
CRON
chmod 644 /etc/cron.d/balce-backup
echo "nightly backup scheduled at 02:30 UTC"
