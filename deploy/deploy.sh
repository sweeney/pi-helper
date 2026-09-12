#!/usr/bin/env bash
set -euo pipefail

REMOTE="${1:?Usage: deploy.sh user@host}"
SERVICE="pi-helper"
BINARY="pi-helper"
BUILD_DIR="dist"
DEPLOY_DIR="/opt/${SERVICE}/bin"
MAIN="./cmd/pi-helper/"
KEEP_VERSIONS=3
ENV_FILE="/run/${SERVICE}.env"
ENV_FILE_TIMEOUT=30

VERSION=$(date +%Y%m%d-%H%M%S)
REMOTE_BIN="${BINARY}-${VERSION}"

echo "=== Detecting remote architecture on $REMOTE ==="
REMOTE_ARCH=$(ssh "$REMOTE" "uname -m")

# Map the remote machine architecture to Go build settings.
GOARM=""
case "$REMOTE_ARCH" in
  aarch64|arm64)  GOOS=linux GOARCH=arm64 ;;
  armv7l)         GOOS=linux GOARCH=arm GOARM=7 ;;
  armv6l)         GOOS=linux GOARCH=arm GOARM=6 ;;
  x86_64|amd64)   GOOS=linux GOARCH=amd64 ;;
  *)
    echo "  x Unsupported remote architecture: $REMOTE_ARCH" >&2
    exit 1
    ;;
esac
echo "  $REMOTE_ARCH -> GOOS=$GOOS GOARCH=$GOARCH${GOARM:+ GOARM=$GOARM}"

echo "=== Building $BINARY for $GOOS/$GOARCH${GOARM:+v$GOARM} ==="
env GOOS=$GOOS GOARCH=$GOARCH ${GOARM:+GOARM=$GOARM} CGO_ENABLED=0 go build \
  -ldflags "-X main.version=${VERSION}" \
  -o "$BUILD_DIR/$BINARY" "$MAIN"

echo "=== Uploading to $REMOTE ==="
scp "$BUILD_DIR/$BINARY" "$REMOTE:$DEPLOY_DIR/$REMOTE_BIN"
ssh "$REMOTE" "chmod 755 $DEPLOY_DIR/$REMOTE_BIN"

echo "=== Activating $REMOTE_BIN ==="
ssh "$REMOTE" "ln -sfn $DEPLOY_DIR/$REMOTE_BIN $DEPLOY_DIR/$BINARY"

# Remove the env file before restarting. /run is tmpfs and survives a service
# restart, so the previous process's file would otherwise linger and make the
# freshness check below pass against stale content.
ssh "$REMOTE" "sudo rm -f $ENV_FILE"
ssh "$REMOTE" "sudo systemctl restart $SERVICE"

echo "=== Verifying ==="
sleep 2
if ! ssh "$REMOTE" "sudo systemctl is-active --quiet $SERVICE"; then
  echo "  x $SERVICE failed to start"
  ssh "$REMOTE" "sudo journalctl -u $SERVICE -n 20 --no-pager"
  exit 1
fi
echo "  v $SERVICE is running"

# Confirm the running binary is the one we just deployed. A service can be
# active while still running a stale binary if the symlink swap or restart
# silently failed, so is-active alone is not enough.
RUNNING_VERSION=$(ssh "$REMOTE" "$DEPLOY_DIR/$BINARY -version" | awk '{print $NF}')
if [ "$RUNNING_VERSION" != "$VERSION" ]; then
  echo "  x version mismatch: expected $VERSION, running $RUNNING_VERSION"
  ssh "$REMOTE" "sudo journalctl -u $SERVICE -n 20 --no-pager"
  exit 1
fi
echo "  v running version $RUNNING_VERSION matches deployed build"

# Confirm the daemon has written its env file (proves it reached steady state).
# The file was deleted before the restart, so its reappearance means this
# process wrote it. The daemon writes once at startup and again on the first
# real state change, and on a Pi wifi can take ~10s to associate, so wait
# rather than checking once.
echo "=== Waiting for $ENV_FILE (up to ${ENV_FILE_TIMEOUT}s) ==="
for i in $(seq "$ENV_FILE_TIMEOUT"); do
  if ssh "$REMOTE" "test -s $ENV_FILE"; then
    echo "  v $ENV_FILE written by the new process after ${i}s"
    ssh "$REMOTE" "cat $ENV_FILE" | sed 's/^/    /'
    ENV_FILE_OK=1
    break
  fi
  sleep 1
done

if [ -z "${ENV_FILE_OK:-}" ]; then
  echo "  x $ENV_FILE not written within ${ENV_FILE_TIMEOUT}s" >&2
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
