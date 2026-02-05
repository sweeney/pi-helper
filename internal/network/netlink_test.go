package network

import (
	"testing"
)

func TestNetlinkHelperFunctions(t *testing.T) {
	// Test isWifiInterface
	wifiTests := []struct {
		name string
		want bool
	}{
		{"wlan0", true},
		{"wlan1", true},
		{"wlp2s0", true},
		{"wlp3s0b1", true},
		{"eth0", false},
		{"enp0s3", false},
		{"lo", false},
	}

	for _, tt := range wifiTests {
		if got := isWifiInterface(tt.name); got != tt.want {
			t.Errorf("isWifiInterface(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// Test isEthernetInterface
	ethTests := []struct {
		name string
		want bool
	}{
		{"eth0", true},
		{"enp0s3", true},
		{"eno1", true},
		{"ens33", true},
		{"wlan0", false},
		{"lo", false},
	}

	for _, tt := range ethTests {
		if got := isEthernetInterface(tt.name); got != tt.want {
			t.Errorf("isEthernetInterface(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
