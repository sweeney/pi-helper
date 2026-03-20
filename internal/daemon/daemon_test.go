package daemon

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sweeney/pi-helper/internal/envwriter"
	"github.com/sweeney/pi-helper/internal/network"
)

// newTestWriter creates a FileWriter for testing.
func newTestWriter(path string) *envwriter.FileWriter {
	return envwriter.New(path)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.EnvFilePath != "/run/pi-helper.env" {
		t.Errorf("DefaultConfig().EnvFilePath = %q, want %q", config.EnvFilePath, "/run/pi-helper.env")
	}
	if config.PollInterval != 30*time.Second {
		t.Errorf("DefaultConfig().PollInterval = %v, want %v", config.PollInterval, 30*time.Second)
	}
	if config.Verbose {
		t.Error("DefaultConfig().Verbose = true, want false")
	}
	if config.WifiRecoveryDelay != 60*time.Second {
		t.Errorf("DefaultConfig().WifiRecoveryDelay = %v, want 60s", config.WifiRecoveryDelay)
	}
	if config.InternetCheckInterval != 5*time.Minute {
		t.Errorf("DefaultConfig().InternetCheckInterval = %v, want 5m", config.InternetCheckInterval)
	}
}

func TestDaemon_Run_WritesInitialState(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	config := Config{
		EnvFilePath:  envPath,
		PollInterval: time.Hour, // Long interval so we don't get spurious updates
		Verbose:      false,
	}

	d := New(config)

	// Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run should write initial state and then exit on context cancel
	_ = d.Run(ctx)

	// Verify env file was created
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	// Should contain network status
	if !strings.Contains(string(content), "NETWORK_STATUS=") {
		t.Error("env file should contain NETWORK_STATUS")
	}
}

func TestDaemon_Run_StopsOnContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	config := Config{
		EnvFilePath:  envPath,
		PollInterval: time.Hour,
		Verbose:      false,
	}

	d := New(config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- d.Run(ctx)
	}()

	// Give daemon time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Daemon should stop
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop within timeout")
	}
}

func TestNew(t *testing.T) {
	config := Config{
		EnvFilePath:  "/tmp/test.env",
		PollInterval: 10 * time.Second,
		Verbose:      true,
	}

	d := New(config)

	if d.config.EnvFilePath != config.EnvFilePath {
		t.Errorf("daemon.config.EnvFilePath = %q, want %q", d.config.EnvFilePath, config.EnvFilePath)
	}
	if d.config.PollInterval != config.PollInterval {
		t.Errorf("daemon.config.PollInterval = %v, want %v", d.config.PollInterval, config.PollInterval)
	}
	if d.config.Verbose != config.Verbose {
		t.Errorf("daemon.config.Verbose = %v, want %v", d.config.Verbose, config.Verbose)
	}
	if d.monitor == nil {
		t.Error("daemon.monitor is nil")
	}
	if d.writer == nil {
		t.Error("daemon.writer is nil")
	}
	if d.logger == nil {
		t.Error("daemon.logger is nil")
	}
}

func TestNew_InternetCheckerEnabled(t *testing.T) {
	config := Config{
		EnvFilePath:           "/tmp/test.env",
		PollInterval:          10 * time.Second,
		InternetCheckInterval: 5 * time.Minute,
	}

	d := New(config)
	if d.internetChecker == nil {
		t.Error("internetChecker should be non-nil when interval > 0")
	}
}

func TestNew_InternetCheckerDisabled(t *testing.T) {
	config := Config{
		EnvFilePath:           "/tmp/test.env",
		PollInterval:          10 * time.Second,
		InternetCheckInterval: 0,
	}

	d := New(config)
	if d.internetChecker != nil {
		t.Error("internetChecker should be nil when interval = 0")
	}
}

func TestNew_InternetCheckerCustomURL(t *testing.T) {
	config := Config{
		EnvFilePath:           "/tmp/test.env",
		PollInterval:          10 * time.Second,
		InternetCheckInterval: 5 * time.Minute,
		InternetCheckURL:      "https://example.com/check",
	}

	d := New(config)
	if d.internetChecker == nil {
		t.Error("internetChecker should be non-nil with custom URL")
	}
}

// --- Wifi recovery tests ---

// newTestDaemon creates a daemon suitable for unit testing wifi recovery
// and state transition logging. It bypasses the real network monitor.
func newTestDaemon(delay time.Duration, recoveryFunc func(context.Context) error) *Daemon {
	return &Daemon{
		config: Config{
			WifiRecoveryDelay: delay,
			RecoveryFunc:      recoveryFunc,
		},
		logger: log.New(os.Stderr, "[test] ", log.LstdFlags),
	}
}

// newTestDaemonWithLog creates a daemon that captures log output for assertions.
func newTestDaemonWithLog(delay time.Duration, recoveryFunc func(context.Context) error) (*Daemon, *bytes.Buffer) {
	var buf bytes.Buffer
	d := &Daemon{
		config: Config{
			WifiRecoveryDelay: delay,
			RecoveryFunc:      recoveryFunc,
		},
		logger: log.New(&buf, "", 0),
	}
	return d, &buf
}

func TestWifiRecovery_DisconnectTriggersTimer(t *testing.T) {
	var called atomic.Int32
	d := newTestDaemon(50*time.Millisecond, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// Simulate wifi disconnect
	ch := d.handleWifiRecovery(context.Background(), network.State{
		Status:     network.StatusDisconnected,
		WifiStatus: network.WifiStatusDisconnected,
	})

	if ch == nil {
		t.Fatal("expected recovery timer channel, got nil")
	}

	// Wait for timer to fire
	<-ch
	d.attemptWifiRecovery(context.Background())

	if called.Load() != 1 {
		t.Errorf("expected recovery func called once, got %d", called.Load())
	}
}

func TestWifiRecovery_ReconnectCancelsTimer(t *testing.T) {
	var called atomic.Int32
	d := newTestDaemon(50*time.Millisecond, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// Simulate wifi disconnect
	ch := d.handleWifiRecovery(context.Background(), network.State{
		Status:     network.StatusDisconnected,
		WifiStatus: network.WifiStatusDisconnected,
	})
	if ch == nil {
		t.Fatal("expected recovery timer channel")
	}

	// Simulate wifi reconnect before timer fires
	ch = d.handleWifiRecovery(context.Background(), network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
	})
	if ch != nil {
		t.Error("expected nil channel after reconnect")
	}

	// Wait a bit to confirm recovery was NOT called
	time.Sleep(100 * time.Millisecond)
	if called.Load() != 0 {
		t.Errorf("recovery func should not have been called, got %d calls", called.Load())
	}
}

func TestWifiRecovery_DisabledWhenDelayZero(t *testing.T) {
	var called atomic.Int32
	d := newTestDaemon(0, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		Status:     network.StatusDisconnected,
		WifiStatus: network.WifiStatusDisconnected,
	})

	if ch != nil {
		t.Error("expected nil channel when recovery is disabled")
	}

	time.Sleep(100 * time.Millisecond)
	if called.Load() != 0 {
		t.Errorf("recovery should not be called when disabled, got %d calls", called.Load())
	}
}

func TestWifiRecovery_ErrorIsLogged(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(50*time.Millisecond, func(ctx context.Context) error {
		return errors.New("nmcli: connection not found")
	})
	d.lastWifiStatus = network.WifiStatusConnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	<-ch
	d.attemptWifiRecovery(context.Background())

	out := logBuf.String()
	if !strings.Contains(out, "recovery failed") {
		t.Errorf("expected 'recovery failed' in log, got: %s", out)
	}
	if !strings.Contains(out, "nmcli: connection not found") {
		t.Errorf("expected error message in log, got: %s", out)
	}
}

func TestWifiRecovery_SuccessIsLogged(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(50*time.Millisecond, func(ctx context.Context) error {
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	<-ch
	d.attemptWifiRecovery(context.Background())

	out := logBuf.String()
	if !strings.Contains(out, "recovery succeeded") {
		t.Errorf("expected 'recovery succeeded' in log, got: %s", out)
	}
}

func TestWifiRecovery_SecondDisconnectResetsTimer(t *testing.T) {
	// A second disconnect while timer is running should replace the timer.
	var called atomic.Int32
	d := newTestDaemon(100*time.Millisecond, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// First disconnect
	ch1 := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	if ch1 == nil {
		t.Fatal("expected timer channel from first disconnect")
	}

	// Brief reconnect
	d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusConnected,
	})

	// Second disconnect — should start a fresh timer
	ch2 := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	if ch2 == nil {
		t.Fatal("expected timer channel from second disconnect")
	}

	// Wait for second timer
	<-ch2
	d.attemptWifiRecovery(context.Background())

	if called.Load() != 1 {
		t.Errorf("expected exactly 1 recovery call, got %d", called.Load())
	}
}

func TestWifiRecovery_MultipleDisconnectReconnectCycles(t *testing.T) {
	var called atomic.Int32
	d := newTestDaemon(30*time.Millisecond, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// Cycle 1: disconnect → reconnect (cancelled)
	d.handleWifiRecovery(context.Background(), network.State{WifiStatus: network.WifiStatusDisconnected})
	d.handleWifiRecovery(context.Background(), network.State{WifiStatus: network.WifiStatusConnected})

	// Cycle 2: disconnect → reconnect (cancelled)
	d.handleWifiRecovery(context.Background(), network.State{WifiStatus: network.WifiStatusDisconnected})
	d.handleWifiRecovery(context.Background(), network.State{WifiStatus: network.WifiStatusConnected})

	// Cycle 3: disconnect → let timer fire
	ch := d.handleWifiRecovery(context.Background(), network.State{WifiStatus: network.WifiStatusDisconnected})
	<-ch
	d.attemptWifiRecovery(context.Background())

	if called.Load() != 1 {
		t.Errorf("expected exactly 1 recovery after 3 cycles, got %d", called.Load())
	}
}

func TestWifiRecovery_NoTransitionKeepsExistingTimer(t *testing.T) {
	// If wifi stays disconnected across multiple state updates (e.g., IP change),
	// the timer should continue running, not reset.
	var called atomic.Int32
	d := newTestDaemon(50*time.Millisecond, func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// Initial disconnect — starts timer
	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	if ch == nil {
		t.Fatal("expected timer channel")
	}

	// Another state change while still disconnected — should return same timer
	ch2 := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
		IP:         "changed",
	})
	if ch2 == nil {
		t.Fatal("expected existing timer channel to be returned")
	}

	// Wait for timer
	<-ch2
	d.attemptWifiRecovery(context.Background())

	if called.Load() != 1 {
		t.Errorf("expected 1 call, got %d", called.Load())
	}
}

func TestWifiRecovery_NilRecoveryFuncUsesDefault(t *testing.T) {
	// When RecoveryFunc is nil, attemptWifiRecovery should call defaultRecoveryFunc.
	// We can't easily test the real nmcli call, but we can verify the code path
	// doesn't panic. The actual call will fail (no nmcli in CI), but that's fine.
	d, logBuf := newTestDaemonWithLog(10*time.Millisecond, nil)
	d.lastWifiStatus = network.WifiStatusConnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	<-ch
	d.attemptWifiRecovery(context.Background())

	out := logBuf.String()
	// Should log either success or failure — the point is it doesn't panic
	if !strings.Contains(out, "wifi: attempting recovery") {
		t.Errorf("expected recovery attempt log, got: %s", out)
	}
}

func TestWifiRecovery_StopClearsTimer(t *testing.T) {
	d := newTestDaemon(time.Hour, func(ctx context.Context) error {
		return nil
	})
	d.lastWifiStatus = network.WifiStatusConnected

	// Start a timer
	d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})

	if d.wifiRecoveryTimer == nil {
		t.Fatal("expected timer to be set")
	}

	// Stop should clear it
	d.stopRecoveryTimer()

	if d.wifiRecoveryTimer != nil {
		t.Error("expected timer to be nil after stop")
	}
}

func TestWifiRecovery_StopIdempotent(t *testing.T) {
	d := newTestDaemon(time.Hour, nil)

	// Calling stop when no timer is set should not panic
	d.stopRecoveryTimer()
	d.stopRecoveryTimer()
}

func TestWifiRecovery_ContextCancelledDuringRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	var calledWithCtx context.Context
	d := newTestDaemon(10*time.Millisecond, func(ctx context.Context) error {
		calledWithCtx = ctx
		return ctx.Err()
	})
	d.lastWifiStatus = network.WifiStatusConnected

	ch := d.handleWifiRecovery(ctx, network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})
	<-ch
	d.attemptWifiRecovery(ctx)

	if calledWithCtx == nil {
		t.Fatal("recovery func should have been called")
	}
	if calledWithCtx.Err() == nil {
		t.Error("expected cancelled context to be passed to recovery func")
	}
}

// --- State transition logging tests ---

func TestLogStateTransition_AllFieldsChanged(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
		IP:         "192.168.1.100",
		WifiSSID:   "HomeNet",
		Type:       network.TypeWifi,
	}

	d.logStateTransition(network.State{
		Status:     network.StatusDisconnected,
		WifiStatus: network.WifiStatusDisconnected,
		IP:         "",
		WifiSSID:   "",
		Type:       network.TypeNone,
	})

	out := logBuf.String()
	for _, want := range []string{
		"status: connected → disconnected",
		"wifi: connected → disconnected",
		"ip: 192.168.1.100 → ",
		"ssid: HomeNet → ",
		"type: wifi → ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogStateTransition_OnlyStatusChanged(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
		IP:         "192.168.1.100",
	}

	d.logStateTransition(network.State{
		Status:     network.StatusDisconnected,
		WifiStatus: network.WifiStatusConnected,
		IP:         "192.168.1.100",
	})

	out := logBuf.String()
	if !strings.Contains(out, "status: connected → disconnected") {
		t.Errorf("expected status change in log, got: %s", out)
	}
	// Should NOT contain wifi/ip/ssid changes
	if strings.Contains(out, "wifi:") {
		t.Errorf("unexpected wifi change in log: %s", out)
	}
	if strings.Contains(out, "ip:") {
		t.Errorf("unexpected ip change in log: %s", out)
	}
}

func TestLogStateTransition_OnlyIPChanged(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status: network.StatusConnected,
		IP:     "192.168.1.100",
	}

	d.logStateTransition(network.State{
		Status: network.StatusConnected,
		IP:     "192.168.1.200",
	})

	out := logBuf.String()
	if !strings.Contains(out, "ip: 192.168.1.100 → 192.168.1.200") {
		t.Errorf("expected IP change in log, got: %s", out)
	}
	if strings.Contains(out, "status:") {
		t.Errorf("unexpected status change in log: %s", out)
	}
}

func TestLogStateTransition_NoChange(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	state := network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
		IP:         "192.168.1.100",
		WifiSSID:   "HomeNet",
		Type:       network.TypeWifi,
	}
	d.lastState = state

	d.logStateTransition(state)

	out := logBuf.String()
	if strings.Contains(out, "state changed") {
		t.Errorf("should not log when nothing changed, got: %s", out)
	}
}

func TestLogStateTransition_SSIDChange(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
		WifiSSID:   "HomeNet",
	}

	d.logStateTransition(network.State{
		Status:     network.StatusConnected,
		WifiStatus: network.WifiStatusConnected,
		WifiSSID:   "GuestNet",
	})

	out := logBuf.String()
	if !strings.Contains(out, "ssid: HomeNet → GuestNet") {
		t.Errorf("expected SSID change in log, got: %s", out)
	}
}

func TestLogStateTransition_TypeChange(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status: network.StatusConnected,
		Type:   network.TypeWifi,
	}

	d.logStateTransition(network.State{
		Status: network.StatusConnected,
		Type:   network.TypeEthernet,
	})

	out := logBuf.String()
	if !strings.Contains(out, "type: wifi → ethernet") {
		t.Errorf("expected type change in log, got: %s", out)
	}
}

// --- joinParts tests ---

func TestJoinParts_Single(t *testing.T) {
	got := joinParts([]string{"status: a → b"})
	want := "status: a → b"
	if got != want {
		t.Errorf("joinParts single: got %q, want %q", got, want)
	}
}

func TestJoinParts_Multiple(t *testing.T) {
	got := joinParts([]string{"status: a → b", "ip: 1 → 2", "wifi: c → d"})
	want := "status: a → b, ip: 1 → 2, wifi: c → d"
	if got != want {
		t.Errorf("joinParts multiple: got %q, want %q", got, want)
	}
}

// --- handleWifiRecovery edge cases ---

func TestWifiRecovery_NoHardwareToDisconnected(t *testing.T) {
	// Transition from no-hardware to disconnected should NOT trigger recovery
	// (this isn't a connected→disconnected transition)
	d := newTestDaemon(10*time.Millisecond, func(ctx context.Context) error {
		t.Error("recovery should not be called")
		return nil
	})
	d.lastWifiStatus = network.WifiStatusNoHardware

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})

	if ch != nil {
		t.Error("expected nil channel for no-hardware → disconnected")
	}
}

func TestWifiRecovery_DisconnectedToDisconnectedNoTimer(t *testing.T) {
	// When already disconnected and no timer is running, should return nil
	d := newTestDaemon(50*time.Millisecond, func(ctx context.Context) error {
		t.Error("recovery should not be called")
		return nil
	})
	d.lastWifiStatus = network.WifiStatusDisconnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusDisconnected,
	})

	if ch != nil {
		t.Error("expected nil channel for disconnected → disconnected with no timer")
	}
}

func TestWifiRecovery_ReconnectWithNoTimer(t *testing.T) {
	// Reconnect when no timer was set (e.g., first connect) should not panic
	d := newTestDaemon(50*time.Millisecond, nil)
	d.lastWifiStatus = network.WifiStatusDisconnected

	ch := d.handleWifiRecovery(context.Background(), network.State{
		WifiStatus: network.WifiStatusConnected,
	})

	if ch != nil {
		t.Error("expected nil channel")
	}
}

func TestWifiRecovery_AttemptClearsTimerField(t *testing.T) {
	d := newTestDaemon(10*time.Millisecond, func(ctx context.Context) error {
		return nil
	})
	d.wifiRecoveryTimer = time.NewTimer(time.Hour) // simulate an active timer

	d.attemptWifiRecovery(context.Background())

	if d.wifiRecoveryTimer != nil {
		t.Error("expected timer to be nil after recovery attempt")
	}
}

// --- NM autoconnect-retries tests ---

// newNMTestDaemon creates a daemon with injectable NM functions for testing.
func newNMTestDaemon(nmFuncs *NMFuncs) (*Daemon, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Daemon{
		config: Config{
			NMFuncs: nmFuncs,
		},
		logger: log.New(&buf, "", 0),
	}, &buf
}

func TestEnsureInfiniteAutoconnectRetries_SetsNonZeroToZero(t *testing.T) {
	var setCalls []struct {
		conn    string
		retries int
	}

	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"HomeNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return -1, nil // default (not infinite)
		},
		SetRetries: func(conn string, retries int) error {
			setCalls = append(setCalls, struct {
				conn    string
				retries int
			}{conn, retries})
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if len(setCalls) != 1 {
		t.Fatalf("expected 1 SetRetries call, got %d", len(setCalls))
	}
	if setCalls[0].conn != "HomeNet" {
		t.Errorf("SetRetries conn: got %q, want %q", setCalls[0].conn, "HomeNet")
	}
	if setCalls[0].retries != 0 {
		t.Errorf("SetRetries retries: got %d, want 0", setCalls[0].retries)
	}

	out := logBuf.String()
	if !strings.Contains(out, "setting autoconnect-retries to infinite (was -1)") {
		t.Errorf("expected 'setting' log, got: %s", out)
	}
	if !strings.Contains(out, "autoconnect-retries set to infinite") {
		t.Errorf("expected 'set to infinite' log, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_AlreadyInfinite(t *testing.T) {
	setCalled := false

	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"HomeNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return 0, nil // already infinite
		},
		SetRetries: func(conn string, retries int) error {
			setCalled = true
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if setCalled {
		t.Error("SetRetries should not be called when already infinite")
	}

	out := logBuf.String()
	if !strings.Contains(out, "already infinite") {
		t.Errorf("expected 'already infinite' log, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_MultipleConnections(t *testing.T) {
	retryMap := map[string]int{
		"HomeNet":  -1, // needs fixing
		"WorkNet":  0,  // already infinite
		"GuestNet": 4,  // needs fixing
	}
	var setConns []string

	d, _ := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"HomeNet", "WorkNet", "GuestNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return retryMap[conn], nil
		},
		SetRetries: func(conn string, retries int) error {
			setConns = append(setConns, conn)
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if len(setConns) != 2 {
		t.Fatalf("expected 2 SetRetries calls, got %d: %v", len(setConns), setConns)
	}
	// HomeNet and GuestNet should be modified, WorkNet skipped
	if setConns[0] != "HomeNet" {
		t.Errorf("first SetRetries should be HomeNet, got %s", setConns[0])
	}
	if setConns[1] != "GuestNet" {
		t.Errorf("second SetRetries should be GuestNet, got %s", setConns[1])
	}
}

func TestEnsureInfiniteAutoconnectRetries_NoConnections(t *testing.T) {
	setCalled := false

	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return nil, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			t.Error("should not be called with no connections")
			return 0, nil
		},
		SetRetries: func(conn string, retries int) error {
			setCalled = true
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if setCalled {
		t.Error("SetRetries should not be called with no connections")
	}

	out := logBuf.String()
	if out != "" {
		t.Errorf("expected no log output, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_ListError(t *testing.T) {
	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return nil, errors.New("nmcli not responding")
		},
		AutoconnectRetries: func(conn string) (int, error) {
			t.Error("should not be called on list error")
			return 0, nil
		},
		SetRetries: func(conn string, retries int) error {
			t.Error("should not be called on list error")
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	out := logBuf.String()
	if !strings.Contains(out, "failed to list NM connections") {
		t.Errorf("expected list error log, got: %s", out)
	}
	if !strings.Contains(out, "nmcli not responding") {
		t.Errorf("expected error message in log, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_ReadRetriesError(t *testing.T) {
	setCalled := false

	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"HomeNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return 0, errors.New("connection not found")
		},
		SetRetries: func(conn string, retries int) error {
			setCalled = true
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if setCalled {
		t.Error("SetRetries should not be called when reading retries fails")
	}

	out := logBuf.String()
	if !strings.Contains(out, "could not read autoconnect-retries") {
		t.Errorf("expected read error log, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_SetRetriesError(t *testing.T) {
	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"HomeNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return -1, nil
		},
		SetRetries: func(conn string, retries int) error {
			return errors.New("permission denied")
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	out := logBuf.String()
	if !strings.Contains(out, "permission denied") {
		t.Errorf("expected set error log, got: %s", out)
	}
}

func TestEnsureInfiniteAutoconnectRetries_ReadErrorContinuesToNext(t *testing.T) {
	// If reading retries fails for one connection, it should still try the next
	var setConns []string

	d, _ := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"BadConn", "GoodConn"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			if conn == "BadConn" {
				return 0, errors.New("read error")
			}
			return 5, nil
		},
		SetRetries: func(conn string, retries int) error {
			setConns = append(setConns, conn)
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	if len(setConns) != 1 || setConns[0] != "GoodConn" {
		t.Errorf("expected SetRetries for GoodConn only, got: %v", setConns)
	}
}

func TestEnsureInfiniteAutoconnectRetries_SpecificRetryCount(t *testing.T) {
	// Verify the "was N" in the log message shows the actual retry count
	d, logBuf := newNMTestDaemon(&NMFuncs{
		WifiConnections: func() ([]string, error) {
			return []string{"MyNet"}, nil
		},
		AutoconnectRetries: func(conn string) (int, error) {
			return 4, nil
		},
		SetRetries: func(conn string, retries int) error {
			return nil
		},
	})

	d.ensureInfiniteAutoconnectRetries()

	out := logBuf.String()
	if !strings.Contains(out, "(was 4)") {
		t.Errorf("expected '(was 4)' in log, got: %s", out)
	}
}

func TestNMFuncs_DefaultsToRealFuncs(t *testing.T) {
	d := &Daemon{config: Config{}}
	fns := d.nmFuncs()

	// Can't easily verify they're the real functions, but we can verify
	// they're non-nil
	if fns.WifiConnections == nil {
		t.Error("WifiConnections should not be nil")
	}
	if fns.AutoconnectRetries == nil {
		t.Error("AutoconnectRetries should not be nil")
	}
	if fns.SetRetries == nil {
		t.Error("SetRetries should not be nil")
	}
}

func TestNMFuncs_UsesInjected(t *testing.T) {
	called := false
	d := &Daemon{
		config: Config{
			NMFuncs: &NMFuncs{
				WifiConnections: func() ([]string, error) {
					called = true
					return nil, nil
				},
				AutoconnectRetries: func(string) (int, error) { return 0, nil },
				SetRetries:         func(string, int) error { return nil },
			},
		},
	}

	fns := d.nmFuncs()
	fns.WifiConnections()

	if !called {
		t.Error("expected injected WifiConnections to be called")
	}
}

// --- handleInternetStatus tests ---

func newInternetTestDaemon() (*Daemon, *bytes.Buffer) {
	tmpDir := os.TempDir()
	var buf bytes.Buffer
	return &Daemon{
		config: Config{
			EnvFilePath: filepath.Join(tmpDir, "internet-test.env"),
		},
		logger: log.New(&buf, "", 0),
		writer: nil, // will set below
		lastState: network.State{
			InternetStatus: network.InternetStatusUnknown,
		},
	}, &buf
}

func TestHandleInternetStatus_ChangesState(t *testing.T) {
	d, logBuf := newInternetTestDaemon()
	tmpDir := t.TempDir()
	d.config.EnvFilePath = filepath.Join(tmpDir, "test.env")
	// Use a real writer so writeState works
	d.writer = newTestWriter(d.config.EnvFilePath)

	d.handleInternetStatus(network.InternetStatusOK)

	if d.lastState.InternetStatus != network.InternetStatusOK {
		t.Errorf("lastState.InternetStatus = %q, want %q", d.lastState.InternetStatus, network.InternetStatusOK)
	}

	out := logBuf.String()
	if !strings.Contains(out, "internet: unknown → ok") {
		t.Errorf("expected transition log, got: %s", out)
	}
}

func TestHandleInternetStatus_NoChangeNoLog(t *testing.T) {
	d, logBuf := newInternetTestDaemon()
	d.lastState.InternetStatus = network.InternetStatusOK

	d.handleInternetStatus(network.InternetStatusOK)

	out := logBuf.String()
	if out != "" {
		t.Errorf("expected no log when status unchanged, got: %s", out)
	}
}

func TestHandleInternetStatus_OKToOffline(t *testing.T) {
	d, logBuf := newInternetTestDaemon()
	tmpDir := t.TempDir()
	d.config.EnvFilePath = filepath.Join(tmpDir, "test.env")
	d.writer = newTestWriter(d.config.EnvFilePath)
	d.lastState.InternetStatus = network.InternetStatusOK

	d.handleInternetStatus(network.InternetStatusOffline)

	if d.lastState.InternetStatus != network.InternetStatusOffline {
		t.Errorf("lastState.InternetStatus = %q, want %q", d.lastState.InternetStatus, network.InternetStatusOffline)
	}

	out := logBuf.String()
	if !strings.Contains(out, "internet: ok → offline") {
		t.Errorf("expected transition log, got: %s", out)
	}
}

func TestHandleInternetStatus_WritesEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	d, _ := newInternetTestDaemon()
	d.config.EnvFilePath = envPath
	d.writer = newTestWriter(envPath)

	d.handleInternetStatus(network.InternetStatusOK)

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}
	if !strings.Contains(string(content), "NETWORK_INTERNET_STATUS=ok") {
		t.Errorf("env file should contain NETWORK_INTERNET_STATUS=ok, got: %s", content)
	}
}

func TestLogStateTransition_InternetStatusChange(t *testing.T) {
	d, logBuf := newTestDaemonWithLog(0, nil)
	d.lastState = network.State{
		Status:         network.StatusConnected,
		InternetStatus: network.InternetStatusOK,
	}

	d.logStateTransition(network.State{
		Status:         network.StatusConnected,
		InternetStatus: network.InternetStatusOffline,
	})

	out := logBuf.String()
	if !strings.Contains(out, "internet: ok → offline") {
		t.Errorf("expected internet change in log, got: %s", out)
	}
	// Should NOT contain status change
	if strings.Contains(out, "status:") {
		t.Errorf("unexpected status change in log: %s", out)
	}
}
