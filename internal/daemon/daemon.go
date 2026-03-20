// Package daemon provides the main daemon loop for pi-helper.
package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/sweeney/pi-helper/internal/envwriter"
	"github.com/sweeney/pi-helper/internal/network"
	"github.com/sweeney/pi-helper/internal/wifi"
)

// Config holds daemon configuration.
type Config struct {
	EnvFilePath          string
	PollInterval         time.Duration
	Verbose              bool
	WifiRecoveryDelay    time.Duration              // Grace period before wifi recovery nudge (0 disables)
	RecoveryFunc         func(context.Context) error // Called to recover wifi; nil uses default nmcli
	NMFuncs              *NMFuncs                    // NM operations; nil uses real nmcli
	InternetCheckInterval time.Duration              // Interval between internet checks (0 disables)
	InternetCheckURL     string                      // URL to check; empty uses default
}

// NMFuncs allows injecting NetworkManager operations for testing.
type NMFuncs struct {
	WifiConnections    func() ([]string, error)
	AutoconnectRetries func(connName string) (int, error)
	SetRetries         func(connName string, retries int) error
}

// DefaultConfig returns the default daemon configuration.
func DefaultConfig() Config {
	return Config{
		EnvFilePath:           "/run/pi-helper.env",
		PollInterval:          30 * time.Second,
		Verbose:               false,
		WifiRecoveryDelay:     60 * time.Second,
		InternetCheckInterval: 5 * time.Minute,
	}
}

// Daemon coordinates the pi-helper components.
type Daemon struct {
	config            Config
	logger            *log.Logger
	monitor           *network.Monitor
	writer            *envwriter.FileWriter
	lastState         network.State
	lastWifiStatus    string
	wifiRecoveryTimer *time.Timer
	internetChecker   *network.InternetChecker
}

// New creates a new Daemon with the given configuration.
func New(config Config) *Daemon {
	logger := log.New(os.Stderr, "[pi-helper] ", log.LstdFlags)

	var checker *network.InternetChecker
	if config.InternetCheckInterval > 0 {
		var opts []network.InternetCheckerOption
		opts = append(opts, network.WithInternetCheckInterval(config.InternetCheckInterval))
		opts = append(opts, network.WithInternetCheckVerbose(config.Verbose))
		if config.InternetCheckURL != "" {
			opts = append(opts, network.WithInternetCheckURL(config.InternetCheckURL))
		}
		checker = network.NewInternetChecker(opts...)
	}

	return &Daemon{
		config:          config,
		logger:          logger,
		monitor:         network.NewMonitor(network.WithPollInterval(config.PollInterval), network.WithVerbose(config.Verbose)),
		writer:          envwriter.New(config.EnvFilePath),
		internetChecker: checker,
	}
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	d.logger.Printf("starting pi-helper daemon")
	d.logger.Printf("writing to: %s", d.config.EnvFilePath)

	// Subscribe to network state changes
	stateCh := d.monitor.Subscribe()

	// Start the network monitor
	if err := d.monitor.Start(ctx); err != nil {
		return err
	}

	// Write initial state
	d.lastState = d.monitor.State()
	d.lastWifiStatus = d.lastState.WifiStatus
	d.writeState(d.lastState)

	d.applyWifiConfig()

	// Start internet checker if enabled
	var internetCh <-chan network.CheckResult
	if d.internetChecker != nil {
		ch := make(chan network.CheckResult, 1)
		internetCh = ch
		go d.internetChecker.Run(ctx, ch)
		d.logger.Printf("internet check enabled every %v", d.config.InternetCheckInterval)
	}

	d.logger.Printf("daemon running, press Ctrl+C to stop")

	// Recovery timer channel (nil until armed)
	var recoveryCh <-chan time.Time

	// Main loop: wait for state changes or shutdown
	for {
		select {
		case <-ctx.Done():
			d.logger.Printf("received shutdown signal, stopping...")
			d.stopRecoveryTimer()
			d.monitor.Stop()
			d.logger.Printf("daemon stopped")
			return nil

		case state, ok := <-stateCh:
			if !ok {
				return nil
			}
			d.logStateTransition(state)
			d.lastState = state
			d.writeState(state)
			recoveryCh = d.handleWifiRecovery(ctx, state)

		case <-recoveryCh:
			recoveryCh = nil
			d.attemptWifiRecovery(ctx)

		case result, ok := <-internetCh:
			if !ok {
				internetCh = nil
				continue
			}
			d.handleInternetCheck(result)
		}
	}
}

// defaultRecoveryFunc runs nmcli to nudge wifi reconnection.
func defaultRecoveryFunc(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "nmcli", "connection", "up", "netplan-wlan0-swee.net").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// logStateTransition logs the state change with before/after details.
func (d *Daemon) logStateTransition(newState network.State) {
	old := d.lastState

	var parts []string
	if old.Status != newState.Status {
		parts = append(parts, fmt.Sprintf("status: %s → %s", old.Status, newState.Status))
	}
	if old.WifiStatus != newState.WifiStatus {
		parts = append(parts, fmt.Sprintf("wifi: %s → %s", old.WifiStatus, newState.WifiStatus))
	}
	if old.IP != newState.IP {
		parts = append(parts, fmt.Sprintf("ip: %s → %s", old.IP, newState.IP))
	}
	if old.WifiSSID != newState.WifiSSID {
		parts = append(parts, fmt.Sprintf("ssid: %s → %s", old.WifiSSID, newState.WifiSSID))
	}
	if old.Type != newState.Type {
		parts = append(parts, fmt.Sprintf("type: %s → %s", old.Type, newState.Type))
	}
	if old.InternetStatus != newState.InternetStatus {
		parts = append(parts, fmt.Sprintf("internet: %s → %s", old.InternetStatus, newState.InternetStatus))
	}

	if len(parts) > 0 {
		d.logger.Printf("network: state changed: %s", joinParts(parts))
	}
}

func joinParts(parts []string) string {
	result := parts[0]
	for _, p := range parts[1:] {
		result += ", " + p
	}
	return result
}

// handleWifiRecovery manages the wifi recovery timer based on state transitions.
// Returns a channel that fires when recovery should be attempted, or nil.
func (d *Daemon) handleWifiRecovery(ctx context.Context, state network.State) <-chan time.Time {
	if d.config.WifiRecoveryDelay == 0 {
		d.lastWifiStatus = state.WifiStatus
		return nil
	}

	prevWifi := d.lastWifiStatus
	d.lastWifiStatus = state.WifiStatus

	// Wifi just disconnected: start recovery timer
	if prevWifi == network.WifiStatusConnected && state.WifiStatus == network.WifiStatusDisconnected {
		d.stopRecoveryTimer()
		d.wifiRecoveryTimer = time.NewTimer(d.config.WifiRecoveryDelay)
		d.logger.Printf("wifi: disconnected, will attempt recovery in %v", d.config.WifiRecoveryDelay)
		return d.wifiRecoveryTimer.C
	}

	// Wifi reconnected: cancel recovery timer
	if prevWifi == network.WifiStatusDisconnected && state.WifiStatus == network.WifiStatusConnected {
		if d.wifiRecoveryTimer != nil {
			d.logger.Printf("wifi: reconnected, cancelled recovery timer")
			d.stopRecoveryTimer()
		}
		return nil
	}

	// No transition: return existing timer channel if active
	if d.wifiRecoveryTimer != nil {
		return d.wifiRecoveryTimer.C
	}
	return nil
}

// attemptWifiRecovery calls the recovery function and logs the result.
func (d *Daemon) attemptWifiRecovery(ctx context.Context) {
	d.wifiRecoveryTimer = nil

	recoveryFn := d.config.RecoveryFunc
	if recoveryFn == nil {
		recoveryFn = defaultRecoveryFunc
	}

	d.logger.Printf("wifi: attempting recovery via nmcli connection up")
	if err := recoveryFn(ctx); err != nil {
		d.logger.Printf("wifi: recovery failed: %v", err)
	} else {
		d.logger.Printf("wifi: recovery succeeded")
	}
}

// stopRecoveryTimer stops and clears the wifi recovery timer.
func (d *Daemon) stopRecoveryTimer() {
	if d.wifiRecoveryTimer != nil {
		d.wifiRecoveryTimer.Stop()
		d.wifiRecoveryTimer = nil
	}
}

// applyWifiConfig disables power save and ensures infinite autoconnect retries
// on all wireless interfaces/connections.
func (d *Daemon) applyWifiConfig() {
	ifaces, err := wifi.WirelessInterfaces()
	if err != nil {
		d.logger.Printf("wifi: failed to list wireless interfaces: %v", err)
		return
	}
	for _, iface := range ifaces {
		on, err := wifi.PowerSaveEnabled(iface)
		if err != nil {
			d.logger.Printf("wifi: %s: could not read power save state: %v", iface, err)
			continue
		}
		if !on {
			d.logger.Printf("wifi: %s: power save already off", iface)
			continue
		}
		d.logger.Printf("wifi: %s: disabling power save...", iface)
		if err := wifi.DisablePowerSave(iface); err != nil {
			d.logger.Printf("wifi: %s: error: %v", iface, err)
		} else {
			d.logger.Printf("wifi: %s: power save disabled", iface)
		}
	}

	d.ensureInfiniteAutoconnectRetries()
}

// nmFuncs returns the NM functions to use, falling back to real nmcli.
func (d *Daemon) nmFuncs() NMFuncs {
	if d.config.NMFuncs != nil {
		return *d.config.NMFuncs
	}
	return NMFuncs{
		WifiConnections:    wifi.NMWifiConnections,
		AutoconnectRetries: wifi.NMAutoconnectRetries,
		SetRetries:         wifi.NMSetAutoconnectRetries,
	}
}

// ensureInfiniteAutoconnectRetries sets autoconnect-retries=0 (infinite) on all
// NM wifi connections that don't already have it. This prevents NM from giving up
// after a fixed number of auth failures.
func (d *Daemon) ensureInfiniteAutoconnectRetries() {
	nm := d.nmFuncs()

	conns, err := nm.WifiConnections()
	if err != nil {
		d.logger.Printf("wifi: failed to list NM connections: %v", err)
		return
	}
	if len(conns) == 0 {
		return
	}

	for _, conn := range conns {
		retries, err := nm.AutoconnectRetries(conn)
		if err != nil {
			d.logger.Printf("wifi: %s: could not read autoconnect-retries: %v", conn, err)
			continue
		}
		if retries == 0 {
			d.logger.Printf("wifi: %s: autoconnect-retries already infinite", conn)
			continue
		}
		d.logger.Printf("wifi: %s: setting autoconnect-retries to infinite (was %d)...", conn, retries)
		if err := nm.SetRetries(conn, 0); err != nil {
			d.logger.Printf("wifi: %s: error: %v", conn, err)
		} else {
			d.logger.Printf("wifi: %s: autoconnect-retries set to infinite", conn)
		}
	}
}

// handleInternetCheck updates the state with the latest internet check result
// and logs diagnostics useful for post-mortem analysis via journalctl.
func (d *Daemon) handleInternetCheck(result network.CheckResult) {
	prev := d.lastState.InternetStatus
	changed := prev != result.Status

	if changed {
		switch {
		case result.Status == network.InternetStatusOffline:
			// Connectivity lost — always log with full detail
			if result.Err != nil {
				d.logger.Printf("internet: %s → %s (error: %v)", prev, result.Status, result.Err)
			} else {
				d.logger.Printf("internet: %s → %s (http %d, %s)", prev, result.Status, result.HTTPStatus, result.Latency.Round(time.Millisecond))
			}
		case result.Status == network.InternetStatusOK:
			// Connectivity restored — log recovery with latency
			d.logger.Printf("internet: %s → %s (http %d, %s)", prev, result.Status, result.HTTPStatus, result.Latency.Round(time.Millisecond))
		default:
			d.logger.Printf("internet: %s → %s", prev, result.Status)
		}

		d.lastState.InternetStatus = result.Status
		d.writeState(d.lastState)
		return
	}

	// No state change — still log failures so every missed check is visible in the journal
	if result.Status == network.InternetStatusOffline {
		if result.Err != nil {
			d.logger.Printf("internet: still offline (error: %v)", result.Err)
		} else {
			d.logger.Printf("internet: still offline (http %d, %s)", result.HTTPStatus, result.Latency.Round(time.Millisecond))
		}
		return
	}

	// Steady-state OK — only log in verbose mode
	if d.config.Verbose {
		d.logger.Printf("internet: ok (http %d, %s)", result.HTTPStatus, result.Latency.Round(time.Millisecond))
	}
}

// writeState writes the current network state to the env file.
func (d *Daemon) writeState(state network.State) {
	vars := state.ToEnvVars()

	if err := d.writer.Write(vars); err != nil {
		d.logger.Printf("error writing env file: %v", err)
		return
	}

	if d.config.Verbose {
		d.logger.Printf("wrote state: status=%s type=%s ip=%s", state.Status, state.Type, state.IP)
	}
}
