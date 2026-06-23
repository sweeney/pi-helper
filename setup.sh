#!/usr/bin/env bash
# One-time host setup. Run as: sudo ./setup.sh
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "Run with sudo: sudo ./setup.sh"
  exit 1
fi

DEPLOY_USER="${SUDO_USER:-sweeney}"
SERVICE="pi-helper"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_FILE="$SCRIPT_DIR/$SERVICE.service"

if [ ! -f "$UNIT_FILE" ]; then
  echo "Error: $SERVICE.service not found next to setup.sh (looked in $SCRIPT_DIR)" >&2
  echo "Run setup.sh from the directory containing $SERVICE.service." >&2
  exit 1
fi

echo "=== Checking runtime dependencies ==="
# iw and nmcli are used by the daemon to disable wifi power save and ensure
# infinite NetworkManager autoconnect-retries. They are not strictly required
# (the daemon logs an error and continues if missing), but the wifi resilience
# features won't work without them.
missing=()
for dep in iw nmcli; do
  if ! command -v "$dep" >/dev/null 2>&1; then
    missing+=("$dep")
  fi
done
if [ "${#missing[@]}" -gt 0 ]; then
  echo "  ! WARNING: missing optional dependencies: ${missing[*]}"
  echo "    The daemon will run, but wifi power-save/autoconnect tuning will be skipped."
  echo "    Install with: sudo apt-get install -y iw network-manager"
else
  echo "  v iw and nmcli present"
fi

echo "=== Creating directories ==="
mkdir -p /opt/$SERVICE/bin
chown "$DEPLOY_USER:$DEPLOY_USER" /opt/$SERVICE/bin
chmod 755 /opt/$SERVICE/bin

echo "=== Installing systemd unit ==="
cp "$UNIT_FILE" /etc/systemd/system/$SERVICE.service
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
