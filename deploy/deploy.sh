#!/usr/bin/env bash
set -euo pipefail

REMOTE="${1:?Usage: deploy.sh user@host}"
SERVICE="pi-helper"
BINARY="pi-helper"
BUILD_DIR="dist"
DEPLOY_DIR="/opt/${SERVICE}/bin"
MAIN="./cmd/pi-helper/"
KEEP_VERSIONS=3

VERSION=$(date +%Y%m%d-%H%M%S)
REMOTE_BIN="${BINARY}-${VERSION}"

echo "=== Building $BINARY for linux/arm64 ==="
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
  -ldflags "-X main.version=${VERSION}" \
  -o "$BUILD_DIR/$BINARY" "$MAIN"

echo "=== Uploading to $REMOTE ==="
scp "$BUILD_DIR/$BINARY" "$REMOTE:$DEPLOY_DIR/$REMOTE_BIN"
ssh "$REMOTE" "chmod 755 $DEPLOY_DIR/$REMOTE_BIN"

echo "=== Activating $REMOTE_BIN ==="
ssh "$REMOTE" "ln -sfn $DEPLOY_DIR/$REMOTE_BIN $DEPLOY_DIR/$BINARY"
ssh "$REMOTE" "sudo systemctl restart $SERVICE"

echo "=== Verifying ==="
sleep 2
if ssh "$REMOTE" "sudo systemctl is-active --quiet $SERVICE"; then
  echo "  v $SERVICE is running"
else
  echo "  x $SERVICE failed to start"
  ssh "$REMOTE" "sudo journalctl -u $SERVICE -n 20 --no-pager"
  exit 1
fi

echo "=== Cleaning old versions (keeping $KEEP_VERSIONS) ==="
ssh "$REMOTE" "
  cd $DEPLOY_DIR &&
  ls -t ${BINARY}-* 2>/dev/null \
    | tail -n +$((KEEP_VERSIONS + 1)) \
    | xargs -r rm --
"

echo ""
echo "=== Deployed $VERSION ==="
ssh "$REMOTE" "sudo journalctl -u $SERVICE -n 10 --no-pager"
