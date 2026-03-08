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
- Go 1.21+ (for building from source)

### First-Time Setup

1. Build the binary for your Pi:
   ```bash
   # For Pi Zero (ARMv6)
   make build-pi-zero

   # For Pi Zero 2, Pi 2/3/4/5 (ARMv7)
   make build-pi-zero2
   ```

2. Copy files to your Pi:
   ```bash
   ssh pi@raspberrypi.local 'mkdir -p ~/pi-helper'
   scp dist/pi-helper-armv7 pi@raspberrypi.local:~/pi-helper/pi-helper
   scp pi-helper.service setup.sh pi@raspberrypi.local:~/pi-helper/
   ```

3. Run setup on the Pi:
   ```bash
   ssh pi@raspberrypi.local 'cd ~/pi-helper && sudo ./setup.sh'
   ```

4. Verify it's running:
   ```bash
   ssh pi@raspberrypi.local 'systemctl status pi-helper && cat /run/pi-helper.env'
   ```

### Subsequent Deploys

After initial setup, deploying updates is simple:

```bash
scp dist/pi-helper-armv7 pi@raspberrypi.local:~/pi-helper/pi-helper
ssh pi@raspberrypi.local 'sudo systemctl restart pi-helper'
```

The setup script creates symlinks from `/usr/local/bin/pi-helper` and `/etc/systemd/system/pi-helper.service` to the files in `~/pi-helper/`, so you only need to copy the new binary and restart the service.

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

# Build for Pi Zero 2 and newer (ARMv7)
make build-pi-zero2

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

# For Pi Zero 2, Pi 3/4/5 (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 go build -o pi-helper ./cmd/pi-helper
```

## Usage

### Command Line

```
pi-helper [flags]

Flags:
  --env-file string          Path to env file (default "/run/pi-helper.env")
  --poll-interval duration   Polling interval (default 30s)
  --verbose                  Enable verbose logging
  --version                  Print version and exit
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
│   └── wifi/                # WiFi power save management
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

5. **WiFi Power Save**: At startup, pi-helper disables WiFi power management on all wireless interfaces (see below).

## WiFi Power Management

### Problem

The Linux kernel's WiFi power save mode allows the wireless driver to sleep the radio aggressively to save power. On a Pi Zero W this can cause:

- MQTT connections dropping silently (the broker times out the client while the radio is asleep)
- The Pi eventually losing network association entirely and becoming unreachable
- No kernel log warnings — from the OS's perspective the interface is still "up"

This is a known issue with the `brcmfmac` driver used by the Pi Zero W's onboard WiFi chip. It is particularly bad for always-on IoT devices that hold long-lived TCP connections.

### Solution

At startup, pi-helper runs `iw dev <iface> set power_save off` on every wireless interface it finds. This instructs the driver to keep the radio active, trading a small amount of power consumption for a stable connection.

### How it works

The `internal/wifi` package provides two functions:

- **`WirelessInterfaces()`** — reads `/sys/class/net` and checks for a `wireless` subdirectory, which the kernel creates for every wireless interface. This is authoritative and requires no external commands.
- **`DisablePowerSave(iface)`** — runs `iw dev <iface> set power_save off`.

`applyWifiConfig()` is called once in the daemon's `Run()` loop, just after writing the initial network state. Failures are logged but do not crash the daemon — if `iw` is absent or the interface doesn't support the setting, pi-helper continues normally.

### Verifying on the device

```bash
# Check the daemon applied the setting at last boot
journalctl -u pi-helper --no-pager | grep wifi
# Expected: wifi: disabled power save on wlan0

# Confirm it is currently off
sudo iw dev wlan0 get power_save
# Expected: Power save: off
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
