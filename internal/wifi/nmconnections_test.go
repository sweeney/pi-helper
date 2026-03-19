package wifi

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestParseNMConnections(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []string
	}{
		{
			name:   "single wifi connection",
			input:  "netplan-wlan0-swee.net:802-11-wireless\n",
			want:   []string{"netplan-wlan0-swee.net"},
		},
		{
			name:   "mixed types",
			input:  "netplan-wlan0-swee.net:802-11-wireless\nWired connection 1:802-3-ethernet\nlo:loopback\n",
			want:   []string{"netplan-wlan0-swee.net"},
		},
		{
			name:   "multiple wifi connections",
			input:  "HomeNet:802-11-wireless\nWorkNet:802-11-wireless\neth0:802-3-ethernet\n",
			want:   []string{"HomeNet", "WorkNet"},
		},
		{
			name:   "no wifi connections",
			input:  "Wired connection 1:802-3-ethernet\nlo:loopback\n",
			want:   nil,
		},
		{
			name:   "empty output",
			input:  "",
			want:   nil,
		},
		{
			name:   "malformed line",
			input:  "no-colon\n",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNMConnections(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseNMConnections() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseNMConnections()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseAutoconnectRetries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "infinite retries",
			input: "connection.autoconnect-retries:0\n",
			want:  0,
		},
		{
			name:  "default retries",
			input: "connection.autoconnect-retries:-1\n",
			want:  -1,
		},
		{
			name:  "specific count",
			input: "connection.autoconnect-retries:5\n",
			want:  5,
		},
		{
			name:  "with other fields",
			input: "connection.id:MyNet\nconnection.autoconnect-retries:0\nconnection.type:802-11-wireless\n",
			want:  0,
		},
		{
			name:  "missing field",
			input: "connection.id:MyNet\n",
			want:  -1,
		},
		{
			name:  "empty",
			input: "",
			want:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAutoconnectRetries(tt.input)
			if got != tt.want {
				t.Errorf("parseAutoconnectRetries() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseNMConnections_ConnectionNameWithColon(t *testing.T) {
	// Connection names can't contain colons in NM, but verify parser
	// handles the SplitN correctly (only splits on first colon)
	input := "My:Net:802-11-wireless\n"
	// SplitN with 2 means name="My", type="Net:802-11-wireless" — won't match
	got := parseNMConnections(input)
	if len(got) != 0 {
		t.Errorf("expected no matches for colon-in-name, got %v", got)
	}
}

func TestParseNMConnections_TrailingNewlines(t *testing.T) {
	// Extra newlines shouldn't produce spurious entries
	input := "HomeNet:802-11-wireless\n\n\n"
	got := parseNMConnections(input)
	if len(got) != 1 || got[0] != "HomeNet" {
		t.Errorf("expected [HomeNet], got %v", got)
	}
}

func TestParseAutoconnectRetries_LargeValue(t *testing.T) {
	got := parseAutoconnectRetries("connection.autoconnect-retries:999\n")
	if got != 999 {
		t.Errorf("expected 999, got %d", got)
	}
}

func TestParseAutoconnectRetries_NonNumeric(t *testing.T) {
	// Sscanf with non-numeric value returns 0
	got := parseAutoconnectRetries("connection.autoconnect-retries:abc\n")
	if got != 0 {
		t.Errorf("expected 0 for non-numeric, got %d", got)
	}
}

func TestNMWifiConnections_NoNmcli(t *testing.T) {
	// If nmcli isn't available (e.g., macOS CI), should return nil, nil
	conns, err := NMWifiConnections()
	if err != nil {
		t.Fatalf("expected nil error when nmcli not found, got: %v", err)
	}
	// conns may be nil (no nmcli) or a list (nmcli present) — both are valid
	_ = conns
}

func TestIsExecNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"exec not found", fmt.Errorf("exec: %w", exec.ErrNotFound), true},
		{"other error", fmt.Errorf("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExecNotFound(tt.err)
			if got != tt.want {
				t.Errorf("isExecNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
