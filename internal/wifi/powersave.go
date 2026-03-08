// Package wifi provides helpers for managing wireless interface settings.
package wifi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const sysClassNet = "/sys/class/net"

// WirelessInterfaces returns names of wireless interfaces by checking
// for /sys/class/net/<name>/wireless (authoritative, no exec needed).
func WirelessInterfaces() ([]string, error) {
	return wirelessInterfacesFrom(sysClassNet)
}

func wirelessInterfacesFrom(sysDir string) ([]string, error) {
	entries, err := os.ReadDir(sysDir)
	if err != nil {
		return nil, fmt.Errorf("wifi: reading %s: %w", sysDir, err)
	}

	var ifaces []string
	for _, e := range entries {
		wirelessPath := filepath.Join(sysDir, e.Name(), "wireless")
		if _, err := os.Stat(wirelessPath); err == nil {
			ifaces = append(ifaces, e.Name())
		}
	}
	return ifaces, nil
}

// PowerSaveEnabled reports whether power save is currently enabled on iface.
// It runs: iw dev <iface> get power_save
func PowerSaveEnabled(iface string) (bool, error) {
	out, err := exec.Command("iw", "dev", iface, "get", "power_save").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("wifi: iw dev %s get power_save: %w: %s", iface, err, out)
	}
	// Output is a single line: "Power save: on" or "Power save: off"
	return strings.Contains(string(out), "Power save: on"), nil
}

// DisablePowerSave runs: iw dev <iface> set power_save off
func DisablePowerSave(iface string) error {
	cmd := exec.Command("iw", "dev", iface, "set", "power_save", "off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wifi: iw dev %s set power_save off: %w: %s", iface, err, out)
	}
	return nil
}
