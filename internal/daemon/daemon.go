// Package daemon provides the main daemon loop for pi-helper.
package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sweeney/pi-helper/internal/envwriter"
	"github.com/sweeney/pi-helper/internal/network"
)

// Config holds daemon configuration.
type Config struct {
	EnvFilePath  string
	PollInterval time.Duration
	Verbose      bool
}

// DefaultConfig returns the default daemon configuration.
func DefaultConfig() Config {
	return Config{
		EnvFilePath:  "/run/pi-helper.env",
		PollInterval: 30 * time.Second,
		Verbose:      false,
	}
}

// Daemon coordinates the pi-helper components.
type Daemon struct {
	config  Config
	logger  *log.Logger
	monitor *network.Monitor
	writer  *envwriter.FileWriter
}

// New creates a new Daemon with the given configuration.
func New(config Config) *Daemon {
	logger := log.New(os.Stderr, "[pi-helper] ", log.LstdFlags)

	return &Daemon{
		config:  config,
		logger:  logger,
		monitor: network.NewMonitor(network.WithPollInterval(config.PollInterval), network.WithVerbose(config.Verbose)),
		writer:  envwriter.New(config.EnvFilePath),
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
	d.writeState(d.monitor.State())

	d.logger.Printf("daemon running, press Ctrl+C to stop")

	// Main loop: wait for state changes or shutdown
	for {
		select {
		case <-ctx.Done():
			d.logger.Printf("received shutdown signal, stopping...")
			d.monitor.Stop()
			d.logger.Printf("daemon stopped")
			return nil

		case state, ok := <-stateCh:
			if !ok {
				return nil
			}
			d.writeState(state)
		}
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
