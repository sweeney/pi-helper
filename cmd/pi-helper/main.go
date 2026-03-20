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
		envFile               = flag.String("env-file", "/run/pi-helper.env", "Path to env file")
		pollInterval          = flag.Duration("poll-interval", 30*time.Second, "Polling interval")
		verbose               = flag.Bool("verbose", false, "Enable verbose logging")
		showVersion           = flag.Bool("version", false, "Print version and exit")
		wifiRecoveryDelay     = flag.Duration("wifi-recovery-delay", 60*time.Second, "Grace period before wifi recovery nudge (0 disables)")
		internetCheckInterval = flag.Duration("internet-check-interval", 5*time.Minute, "Interval between internet connectivity checks (0 disables)")
		internetCheckURL      = flag.String("internet-check-url", "", "URL for internet connectivity check (default: Google connectivity check)")
	)

	flag.Parse()

	if *showVersion {
		fmt.Printf("pi-helper version %s\n", version)
		os.Exit(0)
	}

	config := daemon.Config{
		EnvFilePath:           *envFile,
		PollInterval:          *pollInterval,
		Verbose:               *verbose,
		WifiRecoveryDelay:     *wifiRecoveryDelay,
		InternetCheckInterval: *internetCheckInterval,
		InternetCheckURL:      *internetCheckURL,
	}

	d := daemon.New(config)
	if err := d.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
