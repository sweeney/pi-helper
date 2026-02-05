// Package network provides network monitoring functionality.
package network

// Network status constants.
const (
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
	StatusConnecting   = "connecting"
)

// Network type constants.
const (
	TypeWifi     = "wifi"
	TypeEthernet = "ethernet"
	TypeNone     = ""
)

// Wifi status constants.
const (
	WifiStatusConnected   = "connected"
	WifiStatusDisconnected = "disconnected"
	WifiStatusNoHardware  = "no-hardware"
)

// State represents the current network state.
type State struct {
	Status     string // "connected", "disconnected", "connecting"
	Type       string // "wifi", "ethernet", ""
	IP         string
	Gateway    string
	WifiStatus string // "connected", "disconnected", "no-hardware"
	WifiSSID   string
}

// NewState creates a new State with default disconnected values.
func NewState() State {
	return State{
		Status:     StatusDisconnected,
		Type:       TypeNone,
		WifiStatus: WifiStatusDisconnected,
	}
}

// IsConnected returns true if the network is connected.
func (s State) IsConnected() bool {
	return s.Status == StatusConnected
}

// IsWifi returns true if the primary connection is wifi.
func (s State) IsWifi() bool {
	return s.Type == TypeWifi
}

// IsEthernet returns true if the primary connection is ethernet.
func (s State) IsEthernet() bool {
	return s.Type == TypeEthernet
}

// ToEnvVars converts the state to environment variable key-value pairs.
func (s State) ToEnvVars() map[string]string {
	return map[string]string{
		"NETWORK_STATUS":      s.Status,
		"NETWORK_TYPE":        s.Type,
		"NETWORK_IP":          s.IP,
		"NETWORK_GATEWAY":     s.Gateway,
		"NETWORK_WIFI_STATUS": s.WifiStatus,
		"NETWORK_WIFI_SSID":   s.WifiSSID,
	}
}

// Equal returns true if two states are equal.
func (s State) Equal(other State) bool {
	return s.Status == other.Status &&
		s.Type == other.Type &&
		s.IP == other.IP &&
		s.Gateway == other.Gateway &&
		s.WifiStatus == other.WifiStatus &&
		s.WifiSSID == other.WifiSSID
}
