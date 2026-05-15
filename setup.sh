#!/usr/bin/env bash
# One-time host setup. Run as: sudo ./setup.sh
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "Run with sudo: sudo ./setup.sh"
  exit 1
fi

DEPLOY_USER="${SUDO_USER:-sweeney}"
SERVICE="pi-helper"

echo "=== Creating directories ==="
mkdir -p /opt/$SERVICE/bin
chown "$DEPLOY_USER:$DEPLOY_USER" /opt/$SERVICE/bin
chmod 755 /opt/$SERVICE/bin

echo "=== Installing systemd unit ==="
cp "$(dirname "$0")/pi-helper.service" /etc/systemd/system/$SERVICE.service
chmod 644 /etc/systemd/system/$SERVICE.service

echo "=== Configuring sudoers ==="
cat > /etc/sudoers.d/$SERVICE << EOF
$DEPLOY_USER ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart $SERVICE, /usr/bin/systemctl start $SERVICE, /usr/bin/systemctl stop $SERVICE, /usr/bin/systemctl status $SERVICE, /usr/bin/systemctl is-active $SERVICE, /usr/bin/journalctl -u $SERVICE *
EOF
chmod 440 /etc/sudoers.d/$SERVICE

echo "=== Enabling service ==="
systemctl daemon-reload
systemctl enable $SERVICE

echo ""
echo "Setup complete. Now run: make deploy HOST=<user@host>"
