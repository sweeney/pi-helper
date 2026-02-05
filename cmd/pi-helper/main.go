// pi-helper is a daemon that monitors Raspberry Pi system state and exposes
// simplified environment variables via /run/pi-helper.env.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sweeney/pi-helper/internal/daemon"
)

var version = "dev"

func main() {
	var (
		envFile      = flag.String("env-file", "/run/pi-helper.env", "Path to env file")
		pollInterval = flag.Duration("poll-interval", 30*time.Second, "Polling interval")
		verbose      = flag.Bool("verbose", false, "Enable verbose logging")
		showVersion  = flag.Bool("version", false, "Print version and exit")
	)

	flag.Parse()

	if *showVersion {
		fmt.Printf("pi-helper version %s\n", version)
		os.Exit(0)
	}

	config := daemon.Config{
		EnvFilePath:  *envFile,
		PollInterval: *pollInterval,
		Verbose:      *verbose,
	}

	d := daemon.New(config)
	if err := d.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
