# pi-helper Implementation Plan

## Overview
A Go daemon for Raspberry Pi that monitors system state and exposes simplified environment variables via `/run/pi-helper.env` for other systemd services to consume.

## Target Environment
- Raspberry Pi Zero (ARMv6), Pi Zero 2 (ARMv7+), others
- Raspberry Pi OS Lite (headless)
- Managed via SSH, deployed via scp

## Project Structure

```
pi-helper/
├── cmd/
│   └── pi-helper/
│       └── main.go              # Entry point, flag parsing, daemon startup
├── internal/
│   ├── daemon/
│   │   ├── daemon.go            # Main daemon loop, coordinates helpers
│   │   └── daemon_test.go
│   ├── envwriter/
│   │   ├── writer.go            # Atomic writes to /run/pi-helper.env
│   │   └── writer_test.go
│   └── network/
│       ├── monitor.go           # Network monitoring (netlink + polling)
│       ├── monitor_test.go
│       ├── state.go             # Network state types and transitions
│       ├── state_test.go
│       ├── netlink.go           # Netlink implementation
│       ├── netlink_linux.go     # Linux-specific netlink
│       ├── netlink_stub.go      # Stub for non-Linux (build tag)
│       └── netlink_test.go
├── pkg/
│   └── testutil/
│       └── mocks.go             # Shared test mocks
├── dist/                        # Build output (gitignored)
├── go.mod
├── go.sum
├── Makefile
├── setup.sh                     # Pi setup script
├── pi-helper.service            # Systemd unit file
└── .github/
    └── workflows/
        └── ci.yml               # GitHub Actions
```

## Key Interfaces (for testability)

```go
// internal/network/monitor.go
type NetworkState struct {
    Status     string // "connected", "disconnected", "connecting"
    Type       string // "wifi", "ethernet", ""
    IP         string
    Gateway    string
    WifiStatus string // "connected", "disconnected", "no-hardware"
    WifiSSID   string
    Host       string // system hostname
}

type NetworkMonitor interface {
    Start(ctx context.Context) error
    State() NetworkState
    Subscribe() <-chan NetworkState
    Stop()
}

// internal/envwriter/writer.go
type EnvWriter interface {
    Write(vars map[string]string) error
}
```

## Implementation Details

### 1. Network Monitor
- Uses `vishvananda/netlink` for event-driven monitoring
- Subscribes to: LinkUpdates, AddrUpdates, RouteUpdates
- Polling backup every 30 seconds (configurable)
- Determines primary interface by: has default route > has IP > link up
- Wifi detection via interface name (wlan*) and wireless extensions
- SSID detection via `wpa_cli` or `iwgetid`
- Hostname via `os.Hostname` (portable across all build targets, unlike netlink)

### 2. Env Writer
- Atomic writes: write to temp file, chmod 0644, then rename
- Location: `/run/pi-helper.env` (tmpfs, survives daemon restart but not reboot)
- Format: `KEY=value\n` (shell-compatible)

### 3. Daemon
- Graceful shutdown on SIGTERM/SIGINT
- Coordinates multiple helpers (only network for v1)
- Collects state from helpers, writes combined env file

### 4. Environment Variables (v1)
```
NETWORK_STATUS=connected|disconnected|connecting
NETWORK_TYPE=wifi|ethernet|
NETWORK_IP=192.168.x.x
NETWORK_GATEWAY=192.168.x.1
NETWORK_WIFI_STATUS=connected|disconnected|no-hardware
NETWORK_WIFI_SSID=MyNetwork
NETWORK_HOST=piz2c
```

All keys are always written; an unknown value is an empty string, never an
omitted line. `NETWORK_HOST` comes from `os.Hostname` on every poll and is
validated against the RFC 1123 character set first, so a hostname that would
break a consumer running `source` on the file is published as empty instead.

## Configuration
- **Module path**: `github.com/sweeney/pi-helper`
- **Go version**: 1.21+

## Build & Cross-Compilation

### Makefile targets
```makefile
build:           # Build for current platform
build-pi-zero:   # GOARCH=arm GOARM=6 (ARMv6)
build-pi-zero2:  # GOARCH=arm GOARM=7 (ARMv7)
build-all:       # Build all Pi variants
test:            # go test ./...
lint:            # golangci-lint
clean:           # Remove dist/
```

## GitHub Actions CI

```yaml
- Go 1.21+
- Run: go test ./...
- Run: golangci-lint
- Cross-compile check: build for linux/arm (ARMv6) and linux/arm (ARMv7)
- Upload artifacts (optional)
```

## Systemd Service

```ini
[Unit]
Description=Pi Helper Daemon
After=local-fs.target
Wants=network.target

[Service]
Type=simple
ExecStart=/opt/pi-helper/bin/pi-helper
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Other services consume via:
```ini
[Service]
EnvironmentFile=-/run/pi-helper.env
```

## Testing Strategy

### Unit Tests
- `network/state_test.go`: State transitions, edge cases
- `network/monitor_test.go`: Mock netlink, verify event handling
- `envwriter/writer_test.go`: Atomic writes, formatting, permissions
- `daemon/daemon_test.go`: Lifecycle, signal handling

### Key Test Cases
- File permissions are world-readable (0644)
- Link detection uses IFF_LOWER_UP not IFF_RUNNING
- SSID parsing handles various wpa_cli output formats
- Interface up but not running is not detected as connected
- All state fields populated for wifi connections

### Mocking Approach
- Define interfaces for all external dependencies
- `NetworkMonitor` interface with mock implementation for tests
- Filesystem operations via interface for env writer tests
- Build tags for Linux-specific netlink code

## CLI Flags

```
pi-helper [flags]

Flags:
  --env-file string            Path to env file (default "/run/pi-helper.env")
  --poll-interval duration     Polling interval (default 30s)
  --wifi-recovery-delay duration  Grace period before nudging dropped WiFi (default 60s, 0 disables)
  --verbose                    Enable verbose logging
  --version                    Print version and exit
```

## Deployment

> The README is authoritative for install/deploy. In short: run `sudo ./setup.sh`
> once on the Pi to install to `/opt/pi-helper/bin` and enable the service, then
> `make deploy HOST=user@host` from the repo. `deploy/deploy.sh` auto-detects the
> target architecture, ships a versioned binary, swaps the active symlink, restarts,
> and verifies the running version. The env file is removed before the restart so
the post-deploy check proves the new process wrote it rather than passing on the
previous process's file (`/run` is tmpfs and survives a service restart).

## Implementation Order

1. **Project scaffolding**: go.mod, Makefile, CI workflow, .gitignore
2. **Env writer**: Atomic file writing with tests
3. **Network state types**: State struct, constants, helpers
4. **Network monitor**: Netlink + polling implementation with interfaces
5. **Daemon**: Main loop, signal handling, coordination
6. **CLI**: Flag parsing, entry point
7. **Systemd service file**
8. **Integration tests**

## Verification

1. `go test ./...` passes
2. `make build-pi-zero` produces ARM binary
3. Manual test on actual Pi:
   - scp binary to Pi
   - Run manually, check `/run/pi-helper.env` contents (including `NETWORK_HOST`)
   - Install service, verify starts on boot
   - Disconnect/reconnect wifi, verify env updates
