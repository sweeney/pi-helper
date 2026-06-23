# pi-helper

A lightweight Go daemon for Raspberry Pi that monitors system state and exposes simplified environment variables via `/run/pi-helper.env` for other systemd services to consume.

## Why pi-helper?

When running headless Raspberry Pi projects, other services often need to know about system state—particularly network connectivity. Rather than having each service implement its own monitoring logic, pi-helper provides a single source of truth that services can consume via a simple environment file.

**Use cases:**
- LED status indicators that show network state
- Services that need to wait for network connectivity
- Display scripts showing connection info
- Any systemd service that needs to react to network changes

**Design principles:**
- **Simple**: One daemon, one env file, standard systemd integration
- **Efficient**: Event-driven with netlink, minimal polling as backup
- **Portable**: Runs on Pi Zero (ARMv6) through Pi 5
- **Testable**: Interface-based design with comprehensive tests

See [PLAN.md](PLAN.md) for the full implementation plan and architecture details.

## Environment Variables

pi-helper writes the following variables to `/run/pi-helper.env`:

| Variable | Values | Description |
|----------|--------|-------------|
| `NETWORK_STATUS` | `connected`, `disconnected`, `connecting` | Overall network connectivity |
| `NETWORK_TYPE` | `wifi`, `ethernet`, `` | Primary connection type |
| `NETWORK_IP` | e.g., `192.168.1.100` | IP address of primary interface |
| `NETWORK_GATEWAY` | e.g., `192.168.1.1` | Default gateway |
| `NETWORK_WIFI_STATUS` | `connected`, `disconnected`, `no-hardware` | Wifi-specific status |
| `NETWORK_WIFI_SSID` | e.g., `MyNetwork` | Connected wifi network name |

## Installation

### Prerequisites

- Raspberry Pi running Raspberry Pi OS (or similar Linux)
- Go 1.21+ on your build machine
- On the Pi, for the WiFi resilience features: `iw` and `nmcli` (`sudo apt-get install -y iw network-manager`). These are optional — the daemon runs without them and just skips the WiFi tuning.

### First-Time Setup

`setup.sh` is a one-time host bootstrap: it creates `/opt/pi-helper/bin`, installs the systemd unit, configures a `NOPASSWD` sudoers entry for managing the service, and enables it on boot. It also warns if `iw`/`nmcli` are missing.

1. Copy the setup files to your Pi:
   ```bash
   ssh pi@raspberrypi.local 'mkdir -p ~/pi-helper-setup'
   scp setup.sh pi-helper.service pi@raspberrypi.local:~/pi-helper-setup/
   ```

2. Run setup on the Pi:
   ```bash
   ssh pi@raspberrypi.local 'cd ~/pi-helper-setup && sudo ./setup.sh'
   ```

3. Deploy the binary (see below). The setup files are no longer needed afterward.

### Deploying

After setup, deploy (and update) with a single command from the repo:

```bash
make deploy HOST=pi@raspberrypi.local
```

`deploy/deploy.sh` does everything:

- **Detects the target architecture** over SSH (`uname -m`) and builds the matching binary — `arm64`, `armv7`, `armv6`, or `amd64`. No need to pick a build target by hand.
- Uploads a timestamp-versioned binary to `/opt/pi-helper/bin/pi-helper-<version>` and atomically swaps the `pi-helper` symlink to point at it.
- Restarts the service and **verifies** the deploy: the service is active, the running binary reports the version just built, and `/run/pi-helper.env` has been written.
- Prunes old versioned binaries, keeping the most recent 3 (so rollback is just re-pointing the symlink).

Verify manually any time with:

```bash
ssh pi@raspberrypi.local 'systemctl status pi-helper && cat /run/pi-helper.env'
```

## Building from Source

### Requirements

- Go 1.21 or later
- Make (optional, for convenience)

### Build Commands

```bash
# Build for current platform
make build

# Build for Pi Zero (ARMv6)
make build-pi-zero

# Build for Pi Zero 2 and newer, 32-bit OS (ARMv7)
make build-pi-zero2

# Build for 64-bit Pi OS (Pi 3/4/5, arm64)
make build-arm64

# Build all variants
make build-all

# Run tests
make test

# Clean build artifacts
make clean
```

### Manual Build

```bash
# For Pi Zero (ARMv6)
GOOS=linux GOARCH=arm GOARM=6 go build -o pi-helper ./cmd/pi-helper

# For Pi Zero 2, Pi 2/3/4/5 on a 32-bit OS (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 go build -o pi-helper ./cmd/pi-helper

# For Pi 3/4/5 on a 64-bit OS (arm64)
GOOS=linux GOARCH=arm64 go build -o pi-helper ./cmd/pi-helper
```

For deploys, prefer `make deploy HOST=…`, which picks the right target automatically by inspecting the remote host.

## Usage

### Command Line

```
pi-helper [flags]

Flags:
  --env-file string            Path to env file (default "/run/pi-helper.env")
  --poll-interval duration     Polling interval (default 30s)
  --wifi-recovery-delay duration
                               Grace period before nudging a dropped WiFi
                               connection back up; 0 disables (default 60s)
  --verbose                    Enable verbose logging
  --version                    Print version and exit
```

### Consuming from Other Services

Other systemd services can consume the environment file:

```ini
[Unit]
Description=My Service
After=pi-helper.service
Wants=pi-helper.service

[Service]
Type=simple
EnvironmentFile=-/run/pi-helper.env
ExecStart=/usr/local/bin/my-service
```

The `-` prefix on `EnvironmentFile` makes it optional (service starts even if file doesn't exist yet).

### Example: Shell Script

```bash
#!/bin/bash
source /run/pi-helper.env

if [ "$NETWORK_STATUS" = "connected" ]; then
    echo "Connected via $NETWORK_TYPE at $NETWORK_IP"
else
    echo "Network disconnected"
fi
```

### Example: Python

```python
import os

def load_pi_helper_env():
    env = {}
    try:
        with open('/run/pi-helper.env') as f:
            for line in f:
                if '=' in line:
                    key, value = line.strip().split('=', 1)
                    env[key] = value
    except FileNotFoundError:
        pass
    return env

env = load_pi_helper_env()
if env.get('NETWORK_STATUS') == 'connected':
    print(f"Connected: {env.get('NETWORK_IP')}")
```

## Development

### Project Structure

```
pi-helper/
├── cmd/pi-helper/           # CLI entry point
├── internal/
│   ├── daemon/              # Main daemon loop
│   ├── envwriter/           # Atomic file writing
│   ├── network/             # Network monitoring (netlink + polling)
│   └── wifi/                # WiFi power save + NetworkManager tuning
├── pkg/testutil/            # Shared test mocks
├── Makefile
├── setup.sh                 # Pi setup script
├── pi-helper.service        # Systemd unit file
└── .github/workflows/       # CI configuration
```

### Running Tests

```bash
go test ./...           # Run all tests
go test -v ./...        # Verbose output
go test -race ./...     # With race detector
```

### How It Works

1. **Netlink Events**: On Linux, pi-helper subscribes to netlink for real-time notifications of network changes (link up/down, address changes, route changes).

2. **Polling Backup**: Every 30 seconds (configurable), pi-helper polls the current state as a fallback in case netlink events are missed.

3. **Atomic Writes**: When state changes, pi-helper writes to a temporary file then atomically renames it to `/run/pi-helper.env`, ensuring readers never see partial writes.

4. **Interface Detection**: Determines interface type by name pattern (`wlan*` = wifi, `eth*`/`enp*` = ethernet) and finds the primary interface by checking which has the default route.

5. **WiFi Resilience**: At startup, pi-helper disables WiFi power save and sets NetworkManager's `autoconnect-retries` to infinite on all wireless connections. While running, it nudges a dropped WiFi connection back up via `nmcli` after a grace period (see below).

## WiFi Resilience

Headless Pis on WiFi drop off the network in ways that are hard to diagnose. pi-helper applies three mitigations. All are runtime-only (nothing is persisted to firmware or config files), and all are idempotent — re-checked and re-applied on every start, so they survive reboots as long as pi-helper is enabled. Each requires an external tool (`iw` or `nmcli`); if a tool is absent the daemon logs it and continues without that mitigation.

The work happens in `applyWifiConfig()`, called once in the daemon's `Run()` loop just after writing the initial network state, plus a recovery timer that runs while the daemon is up.

### 1. Disable WiFi power save

The Linux kernel's WiFi power save mode lets the wireless driver sleep the radio aggressively. On a Pi Zero W (the `brcmfmac` driver) this can silently drop long-lived TCP connections (e.g. an MQTT broker times the client out while the radio sleeps), and the Pi can eventually lose association entirely and become unreachable — with no kernel log warning, since the interface still looks "up".

At startup pi-helper runs `iw dev <iface> set power_save off` on every wireless interface. Wireless interfaces are found via `WirelessInterfaces()`, which reads `/sys/class/net` for a `wireless` subdirectory (authoritative, no external command needed).

### 2. Infinite NetworkManager autoconnect-retries

By default NetworkManager gives up reconnecting a connection after a finite number of `autoconnect-retries`. On a flaky link the Pi can exhaust them and stay offline indefinitely. pi-helper sets `autoconnect-retries` to `0` (infinite) on every `802-11-wireless` connection via `nmcli`, so NetworkManager keeps trying forever.

### 3. Active recovery nudge

While running, pi-helper watches connectivity. When WiFi drops, it waits a grace period (`--wifi-recovery-delay`, default 60s; set to `0` to disable) and, if still down, runs `nmcli connection up` to nudge the connection back. If WiFi recovers on its own first, the pending recovery is cancelled.

### Verifying on the device

```bash
# What the daemon applied at last start
journalctl -u pi-helper --no-pager | grep wifi
# Expected (first run):  wifi: wlan0: power save disabled
#                        wifi: <conn>: autoconnect-retries set to infinite
# Expected (later runs): wifi: wlan0: power save already off
#                        wifi: <conn>: autoconnect-retries already infinite

# Confirm power save is currently off
sudo iw dev wlan0 get power_save
# Expected: Power save: off

# Confirm autoconnect-retries is infinite (0)
nmcli -t -f connection.autoconnect-retries connection show <connection-name>
# Expected: connection.autoconnect-retries:0
```

Note: this setting is applied at runtime, not persisted in firmware. It is re-applied every time pi-helper starts, so it survives reboots as long as pi-helper is enabled as a systemd service.

## Project Statistics

This project was written entirely by Claude (Opus 4.5) in a single session.

| Metric | Value |
|--------|-------|
| **Source code** | 1,094 lines |
| **Test code** | 1,089 lines |
| **Config/scripts** | 404 lines |
| **Total Go code** | 2,183 lines |
| **Test coverage** | Unit tests for all packages |
| **Time to implement** | ~45 minutes |
| **Estimated cost** | ~$15 |

### File Breakdown

| File | Lines | Purpose |
|------|-------|---------|
| `network/monitor_test.go` | 548 | Monitor tests |
| `network/monitor.go` | 384 | Network state monitoring |
| `envwriter/writer_test.go` | 202 | Env writer tests |
| `network/state_test.go` | 173 | State type tests |
| `network/netlink_linux.go` | 150 | Linux netlink implementation |
| `testutil/mocks.go` | 123 | Shared test mocks |
| `daemon/daemon_test.go` | 120 | Daemon tests |
| `daemon/daemon.go` | 104 | Main daemon loop |
| `envwriter/writer.go` | 94 | Atomic file writing |
| `network/state.go` | 79 | Network state types |
| `network/netlink.go` | 79 | Netlink interface definitions |

## License

MIT

## Contributing

Contributions welcome! Please ensure tests pass (`make test`) before submitting PRs.
