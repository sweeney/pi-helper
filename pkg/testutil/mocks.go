// Package testutil provides shared test utilities and mocks.
package testutil

import (
	"net"

	"github.com/sweeney/pi-helper/internal/network"
)

// MockNetlinkProvider implements network.NetlinkProvider for testing.
type MockNetlinkProvider struct {
	Links_  []network.LinkInfo
	Addrs_  map[int][]network.AddrInfo
	Routes_ []network.RouteInfo

	SubscribeErr error
	LinksErr     error
	AddrsErr     error
	RoutesErr    error

	LinkCh  chan network.LinkInfo
	AddrCh  chan network.AddrInfo
	RouteCh chan network.RouteInfo
}

// NewMockNetlinkProvider creates a new mock provider with initialized channels.
func NewMockNetlinkProvider() *MockNetlinkProvider {
	return &MockNetlinkProvider{
		Addrs_:  make(map[int][]network.AddrInfo),
		LinkCh:  make(chan network.LinkInfo, 10),
		AddrCh:  make(chan network.AddrInfo, 10),
		RouteCh: make(chan network.RouteInfo, 10),
	}
}

// Links returns the mock links.
func (m *MockNetlinkProvider) Links() ([]network.LinkInfo, error) {
	if m.LinksErr != nil {
		return nil, m.LinksErr
	}
	return m.Links_, nil
}

// Addrs returns the mock addresses for a link.
func (m *MockNetlinkProvider) Addrs(linkIndex int) ([]network.AddrInfo, error) {
	if m.AddrsErr != nil {
		return nil, m.AddrsErr
	}
	return m.Addrs_[linkIndex], nil
}

// Routes returns the mock routes.
func (m *MockNetlinkProvider) Routes() ([]network.RouteInfo, error) {
	if m.RoutesErr != nil {
		return nil, m.RoutesErr
	}
	return m.Routes_, nil
}

// Subscribe returns mock channels for updates.
func (m *MockNetlinkProvider) Subscribe() (<-chan network.LinkInfo, <-chan network.AddrInfo, <-chan network.RouteInfo, chan struct{}, error) {
	if m.SubscribeErr != nil {
		return nil, nil, nil, nil, m.SubscribeErr
	}
	done := make(chan struct{})
	return m.LinkCh, m.AddrCh, m.RouteCh, done, nil
}

// SetConnected configures the mock to simulate a connected state.
func (m *MockNetlinkProvider) SetConnected(ifname string, ip string, gateway string) {
	ifType := "eth"
	if len(ifname) >= 4 && ifname[:4] == "wlan" {
		ifType = "wifi"
	}

	m.Links_ = []network.LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: ifname, Index: 2, Up: true, Running: true},
	}

	m.Addrs_ = map[int][]network.AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}},
	}

	m.Routes_ = []network.RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP(gateway)},
	}

	_ = ifType // Used for documentation
}

// SetDisconnected configures the mock to simulate a disconnected state.
func (m *MockNetlinkProvider) SetDisconnected() {
	m.Links_ = []network.LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
	}
	m.Addrs_ = make(map[int][]network.AddrInfo)
	m.Routes_ = nil
}

// MockEnvWriter implements envwriter.Writer for testing.
type MockEnvWriter struct {
	Vars    map[string]string
	WriteErr error
	WriteCalls int
}

// NewMockEnvWriter creates a new mock env writer.
func NewMockEnvWriter() *MockEnvWriter {
	return &MockEnvWriter{
		Vars: make(map[string]string),
	}
}

// Write stores the vars for inspection.
func (m *MockEnvWriter) Write(vars map[string]string) error {
	if m.WriteErr != nil {
		return m.WriteErr
	}
	m.WriteCalls++
	m.Vars = vars
	return nil
}
