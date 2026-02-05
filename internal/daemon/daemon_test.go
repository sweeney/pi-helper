package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
