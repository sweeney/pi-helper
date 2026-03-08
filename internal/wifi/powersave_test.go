package wifi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWirelessInterfaces(t *testing.T) {
	tmp := t.TempDir()

	// wlan0 should be detected (has wireless subdir)
	if err := os.MkdirAll(filepath.Join(tmp, "wlan0", "wireless"), 0755); err != nil {
		t.Fatal(err)
	}
	// eth0 should be filtered out (no wireless subdir)
	if err := os.MkdirAll(filepath.Join(tmp, "eth0"), 0755); err != nil {
		t.Fatal(err)
	}

	ifaces, err := wirelessInterfacesFrom(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0] != "wlan0" {
		t.Errorf("expected [wlan0], got %v", ifaces)
	}
}

func TestDisablePowerSave_Error(t *testing.T) {
	err := DisablePowerSave("nonexistent0")
	if err == nil {
		t.Fatal("expected error when iw not found or interface missing, got nil")
	}
}

func TestPowerSaveEnabled_Error(t *testing.T) {
	_, err := PowerSaveEnabled("nonexistent0")
	if err == nil {
		t.Fatal("expected error when iw not found or interface missing, got nil")
	}
}
