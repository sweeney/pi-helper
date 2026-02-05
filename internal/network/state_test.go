package network

import (
	"testing"
)

func TestNewState(t *testing.T) {
	state := NewState()

	if state.Status != StatusDisconnected {
		t.Errorf("NewState().Status = %q, want %q", state.Status, StatusDisconnected)
	}
	if state.Type != TypeNone {
		t.Errorf("NewState().Type = %q, want %q", state.Type, TypeNone)
	}
	if state.WifiStatus != WifiStatusDisconnected {
		t.Errorf("NewState().WifiStatus = %q, want %q", state.WifiStatus, WifiStatusDisconnected)
	}
}

func TestState_IsConnected(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"connected", StatusConnected, true},
		{"disconnected", StatusDisconnected, false},
		{"connecting", StatusConnecting, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := State{Status: tt.status}
			if got := s.IsConnected(); got != tt.want {
				t.Errorf("IsConnected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_IsWifi(t *testing.T) {
	tests := []struct {
		name     string
		connType string
		want     bool
	}{
		{"wifi", TypeWifi, true},
		{"ethernet", TypeEthernet, false},
		{"none", TypeNone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := State{Type: tt.connType}
			if got := s.IsWifi(); got != tt.want {
				t.Errorf("IsWifi() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_IsEthernet(t *testing.T) {
	tests := []struct {
		name     string
		connType string
		want     bool
	}{
		{"ethernet", TypeEthernet, true},
		{"wifi", TypeWifi, false},
		{"none", TypeNone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := State{Type: tt.connType}
			if got := s.IsEthernet(); got != tt.want {
				t.Errorf("IsEthernet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_ToEnvVars(t *testing.T) {
	state := State{
		Status:     StatusConnected,
		Type:       TypeWifi,
		IP:         "192.168.1.100",
		Gateway:    "192.168.1.1",
		WifiStatus: WifiStatusConnected,
		WifiSSID:   "MyNetwork",
	}

	vars := state.ToEnvVars()

	expected := map[string]string{
		"NETWORK_STATUS":      "connected",
		"NETWORK_TYPE":        "wifi",
		"NETWORK_IP":          "192.168.1.100",
		"NETWORK_GATEWAY":     "192.168.1.1",
		"NETWORK_WIFI_STATUS": "connected",
		"NETWORK_WIFI_SSID":   "MyNetwork",
	}

	for k, want := range expected {
		if got, ok := vars[k]; !ok {
			t.Errorf("ToEnvVars() missing key %q", k)
		} else if got != want {
			t.Errorf("ToEnvVars()[%q] = %q, want %q", k, got, want)
		}
	}

	if len(vars) != len(expected) {
		t.Errorf("ToEnvVars() returned %d vars, want %d", len(vars), len(expected))
	}
}

func TestState_ToEnvVars_Empty(t *testing.T) {
	state := NewState()
	vars := state.ToEnvVars()

	if vars["NETWORK_STATUS"] != StatusDisconnected {
		t.Errorf("ToEnvVars()[NETWORK_STATUS] = %q, want %q", vars["NETWORK_STATUS"], StatusDisconnected)
	}
	if vars["NETWORK_TYPE"] != TypeNone {
		t.Errorf("ToEnvVars()[NETWORK_TYPE] = %q, want %q", vars["NETWORK_TYPE"], TypeNone)
	}
}

func TestState_Equal(t *testing.T) {
	state1 := State{
		Status:     StatusConnected,
		Type:       TypeWifi,
		IP:         "192.168.1.100",
		Gateway:    "192.168.1.1",
		WifiStatus: WifiStatusConnected,
		WifiSSID:   "MyNetwork",
	}

	state2 := State{
		Status:     StatusConnected,
		Type:       TypeWifi,
		IP:         "192.168.1.100",
		Gateway:    "192.168.1.1",
		WifiStatus: WifiStatusConnected,
		WifiSSID:   "MyNetwork",
	}

	if !state1.Equal(state2) {
		t.Error("Equal() returned false for equal states")
	}

	// Test inequality for each field
	tests := []struct {
		name  string
		state State
	}{
		{"different status", State{Status: StatusDisconnected, Type: TypeWifi, IP: "192.168.1.100", Gateway: "192.168.1.1", WifiStatus: WifiStatusConnected, WifiSSID: "MyNetwork"}},
		{"different type", State{Status: StatusConnected, Type: TypeEthernet, IP: "192.168.1.100", Gateway: "192.168.1.1", WifiStatus: WifiStatusConnected, WifiSSID: "MyNetwork"}},
		{"different ip", State{Status: StatusConnected, Type: TypeWifi, IP: "192.168.1.101", Gateway: "192.168.1.1", WifiStatus: WifiStatusConnected, WifiSSID: "MyNetwork"}},
		{"different gateway", State{Status: StatusConnected, Type: TypeWifi, IP: "192.168.1.100", Gateway: "192.168.1.2", WifiStatus: WifiStatusConnected, WifiSSID: "MyNetwork"}},
		{"different wifi status", State{Status: StatusConnected, Type: TypeWifi, IP: "192.168.1.100", Gateway: "192.168.1.1", WifiStatus: WifiStatusDisconnected, WifiSSID: "MyNetwork"}},
		{"different ssid", State{Status: StatusConnected, Type: TypeWifi, IP: "192.168.1.100", Gateway: "192.168.1.1", WifiStatus: WifiStatusConnected, WifiSSID: "OtherNetwork"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if state1.Equal(tt.state) {
				t.Errorf("Equal() returned true for %s", tt.name)
			}
		})
	}
}
