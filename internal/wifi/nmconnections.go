package wifi

import (
	"fmt"
	"os/exec"
	"strings"
)

// NMWifiConnections returns the names of NetworkManager connections whose type
// is "802-11-wireless". Returns nil (not an error) if nmcli is not installed.
func NMWifiConnections() ([]string, error) {
	// --terse gives machine-readable output: NAME:UUID:TYPE:DEVICE
	out, err := exec.Command("nmcli", "--terse", "--fields", "NAME,TYPE", "connection", "show").CombinedOutput()
	if err != nil {
		if isExecNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nmcli connection show: %w: %s", err, out)
	}
	return parseNMConnections(string(out)), nil
}

// parseNMConnections extracts wifi connection names from nmcli --terse output.
// Each line is "NAME:TYPE" (colon-separated).
func parseNMConnections(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == "802-11-wireless" {
			names = append(names, parts[0])
		}
	}
	return names
}

// NMAutoconnectRetries returns the autoconnect-retries value for a connection.
// 0 means infinite, -1 means default (usually 4).
func NMAutoconnectRetries(connName string) (int, error) {
	out, err := exec.Command("nmcli", "--terse", "--fields", "connection.autoconnect-retries",
		"connection", "show", connName).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("nmcli connection show %q: %w: %s", connName, err, out)
	}
	return parseAutoconnectRetries(string(out)), nil
}

// parseAutoconnectRetries extracts the integer value from nmcli --terse output.
// The line is "connection.autoconnect-retries:0" or "connection.autoconnect-retries:-1".
func parseAutoconnectRetries(output string) int {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.HasPrefix(line, "connection.autoconnect-retries:") {
			val := strings.TrimPrefix(line, "connection.autoconnect-retries:")
			var n int
			fmt.Sscanf(val, "%d", &n)
			return n
		}
	}
	return -1
}

// NMSetAutoconnectRetries sets autoconnect-retries for a connection.
// Use 0 for infinite retries.
func NMSetAutoconnectRetries(connName string, retries int) error {
	out, err := exec.Command("nmcli", "connection", "modify", connName,
		"connection.autoconnect-retries", fmt.Sprintf("%d", retries)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nmcli connection modify %q: %w: %s", connName, err, out)
	}
	return nil
}

func isExecNotFound(err error) bool {
	return strings.Contains(err.Error(), exec.ErrNotFound.Error())
}
