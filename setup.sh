#!/bin/bash
#
# Setup script for pi-helper on Raspberry Pi
# Run this from ~/pi-helper/ after copying files from dev machine
#

set -e

# Require sudo
if [ "$EUID" -ne 0 ]; then
    echo "Error: This script must be run with sudo"
    echo "Usage: sudo ./setup.sh"
    exit 1
fi

# Get the actual user (not root)
ACTUAL_USER="${SUDO_USER:-$USER}"
INSTALL_DIR="/home/${ACTUAL_USER}/pi-helper"

# Verify we're in the right place
if [ ! -f "${INSTALL_DIR}/pi-helper" ]; then
    echo "Error: pi-helper binary not found in ${INSTALL_DIR}"
    echo "Make sure you've copied the binary from your dev machine first:"
    echo "  scp dist/pi-helper-armv7 pi@<PI_IP>:~/pi-helper/pi-helper"
    exit 1
fi

if [ ! -f "${INSTALL_DIR}/pi-helper.service" ]; then
    echo "Error: pi-helper.service not found in ${INSTALL_DIR}"
    echo "Make sure you've copied the service file from your dev machine first:"
    echo "  scp pi-helper.service pi@<PI_IP>:~/pi-helper/"
    exit 1
fi

echo "Setting up pi-helper..."

# Make binary executable
chmod +x "${INSTALL_DIR}/pi-helper"
echo "  Made binary executable"

# Create symlinks
ln -sf "${INSTALL_DIR}/pi-helper" /usr/local/bin/pi-helper
echo "  Created symlink: /usr/local/bin/pi-helper"

ln -sf "${INSTALL_DIR}/pi-helper.service" /etc/systemd/system/pi-helper.service
echo "  Created symlink: /etc/systemd/system/pi-helper.service"

# Enable and start service
systemctl daemon-reload
systemctl enable pi-helper
systemctl start pi-helper
echo "  Enabled and started pi-helper service"

echo ""
echo "Setup complete! Check status with:"
echo "  systemctl status pi-helper"
echo "  journalctl -u pi-helper -f"
echo "  cat /run/pi-helper.env"
