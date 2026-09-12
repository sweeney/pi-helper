package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockNetlinkProvider implements NetlinkProvider for testing.
type MockNetlinkProvider struct {
	mu     sync.RWMutex
	links  []LinkInfo
	addrs  map[int][]AddrInfo
	routes []RouteInfo

	subscribeErr error
	linksErr     error
	addrsErr     error
	routesErr    error

	linkCh  chan LinkInfo
	addrCh  chan AddrInfo
	routeCh chan RouteInfo
}

func NewMockNetlinkProvider() *MockNetlinkProvider {
	return &MockNetlinkProvider{
		addrs:   make(map[int][]AddrInfo),
		linkCh:  make(chan LinkInfo, 10),
		addrCh:  make(chan AddrInfo, 10),
		routeCh: make(chan RouteInfo, 10),
	}
}

func (m *MockNetlinkProvider) Links() ([]LinkInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.linksErr != nil {
		return nil, m.linksErr
	}
	return m.links, nil
}

func (m *MockNetlinkProvider) Addrs(linkIndex int) ([]AddrInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.addrsErr != nil {
		return nil, m.addrsErr
	}
	return m.addrs[linkIndex], nil
}

func (m *MockNetlinkProvider) Routes() ([]RouteInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.routesErr != nil {
		return nil, m.routesErr
	}
	return m.routes, nil
}

func (m *MockNetlinkProvider) Subscribe() (<-chan LinkInfo, <-chan AddrInfo, <-chan RouteInfo, chan struct{}, error) {
	if m.subscribeErr != nil {
		return nil, nil, nil, nil, m.subscribeErr
	}
	done := make(chan struct{})
	return m.linkCh, m.addrCh, m.routeCh, done, nil
}

// SetState safely sets the mock state (for use during tests with running monitors).
func (m *MockNetlinkProvider) SetState(links []LinkInfo, addrs map[int][]AddrInfo, routes []RouteInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links = links
	m.addrs = addrs
	m.routes = routes
}

func TestMonitor_State_Disconnected(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Status != StatusDisconnected {
		t.Errorf("State().Status = %q, want %q", state.Status, StatusDisconnected)
	}
}

func TestMonitor_State_WifiConnected(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "wlan0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Status != StatusConnected {
		t.Errorf("State().Status = %q, want %q", state.Status, StatusConnected)
	}
	if state.Type != TypeWifi {
		t.Errorf("State().Type = %q, want %q", state.Type, TypeWifi)
	}
	if state.IP != "192.168.1.100" {
		t.Errorf("State().IP = %q, want %q", state.IP, "192.168.1.100")
	}
	if state.Gateway != "192.168.1.1" {
		t.Errorf("State().Gateway = %q, want %q", state.Gateway, "192.168.1.1")
	}
	if state.WifiStatus != WifiStatusConnected {
		t.Errorf("State().WifiStatus = %q, want %q", state.WifiStatus, WifiStatusConnected)
	}
}

func TestMonitor_State_EthernetConnected(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "eth0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Status != StatusConnected {
		t.Errorf("State().Status = %q, want %q", state.Status, StatusConnected)
	}
	if state.Type != TypeEthernet {
		t.Errorf("State().Type = %q, want %q", state.Type, TypeEthernet)
	}
	if state.IP != "192.168.1.50" {
		t.Errorf("State().IP = %q, want %q", state.IP, "192.168.1.50")
	}
}

func TestMonitor_State_NoWifiHardware(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "eth0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.WifiStatus != WifiStatusNoHardware {
		t.Errorf("State().WifiStatus = %q, want %q", state.WifiStatus, WifiStatusNoHardware)
	}
}

func TestMonitor_Subscribe(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.SetState(
		[]LinkInfo{{Name: "lo", Index: 1, Up: true, Running: true}},
		make(map[int][]AddrInfo),
		nil,
	)

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(50*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := m.Subscribe()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	// Wait a bit for initial poll to complete, then update mock
	time.Sleep(10 * time.Millisecond)

	// Update mock to simulate network coming up (thread-safe)
	mock.SetState(
		[]LinkInfo{
			{Name: "lo", Index: 1, Up: true, Running: true},
			{Name: "eth0", Index: 2, Up: true, Running: true},
		},
		map[int][]AddrInfo{
			2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
		},
		[]RouteInfo{
			{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
		},
	)

	// Wait for state change notification - may receive disconnected first, wait for connected
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case state := <-sub:
			if state.Status == StatusConnected {
				// Success
				return
			}
			// Keep waiting for connected state
		case <-timeout:
			t.Error("timed out waiting for connected state notification")
			return
		}
	}
}

func TestMonitor_Stop(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Stop() timed out")
	}
}

func TestIsWifiInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"wlan0", true},
		{"wlan1", true},
		{"wlp2s0", true},
		{"wlp3s0", true},
		{"eth0", false},
		{"enp0s3", false},
		{"lo", false},
		{"docker0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWifiInterface(tt.name); got != tt.want {
				t.Errorf("isWifiInterface(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsEthernetInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"eth0", true},
		{"eth1", true},
		{"enp0s3", true},
		{"eno1", true},
		{"ens33", true},
		{"wlan0", false},
		{"lo", false},
		{"docker0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEthernetInterface(tt.name); got != tt.want {
				t.Errorf("isEthernetInterface(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsDefaultRoute(t *testing.T) {
	tests := []struct {
		name  string
		route RouteInfo
		want  bool
	}{
		{
			name:  "nil destination",
			route: RouteInfo{Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
			want:  true,
		},
		{
			name:  "0.0.0.0/0",
			route: RouteInfo{Dst: &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}, Gateway: net.ParseIP("192.168.1.1")},
			want:  true,
		},
		{
			name:  "specific destination",
			route: RouteInfo{Dst: &net.IPNet{IP: net.ParseIP("192.168.1.0"), Mask: net.CIDRMask(24, 32)}, Gateway: net.ParseIP("192.168.1.1")},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDefaultRoute(tt.route); got != tt.want {
				t.Errorf("isDefaultRoute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMonitor_State_LinkUpButNotRunning(t *testing.T) {
	// Regression test: interface that is Up but not Running should not be detected
	// This caught the IFF_RUNNING vs IFF_LOWER_UP bug
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "wlan0", Index: 2, Up: true, Running: false}, // Up but not Running
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	// Should be disconnected because wlan0 is not Running
	if state.Status != StatusDisconnected {
		t.Errorf("State().Status = %q, want %q (interface Up but not Running should be ignored)", state.Status, StatusDisconnected)
	}
}

func TestMonitor_State_AllFieldsPopulated(t *testing.T) {
	// Verify all fields are populated for a connected wifi state
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "wlan0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()

	// Check all fields that should be non-empty for wifi connection
	if state.Status == "" {
		t.Error("State().Status is empty")
	}
	if state.Type == "" {
		t.Error("State().Type is empty for wifi connection")
	}
	if state.IP == "" {
		t.Error("State().IP is empty")
	}
	if state.Gateway == "" {
		t.Error("State().Gateway is empty")
	}
	if state.WifiStatus == "" {
		t.Error("State().WifiStatus is empty")
	}
	// Note: WifiSSID may be empty if wpa_cli/iwgetid not available, but should be tested separately
}

func TestParseWpaCliOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "typical output",
			output: `bssid=aa:bb:cc:dd:ee:ff
freq=2437
ssid=MyNetwork
id=0
mode=station
pairwise_cipher=CCMP
group_cipher=CCMP
key_mgmt=WPA2-PSK
wpa_state=COMPLETED
ip_address=192.168.1.100
`,
			want: "MyNetwork",
		},
		{
			name: "ssid with spaces",
			output: `bssid=aa:bb:cc:dd:ee:ff
ssid=My Home Network
wpa_state=COMPLETED
`,
			want: "My Home Network",
		},
		{
			name:   "no ssid line",
			output: "bssid=aa:bb:cc:dd:ee:ff\nwpa_state=COMPLETED\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "ssid= with empty value",
			output: "ssid=\nwpa_state=COMPLETED\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWpaCliSSID(tt.output)
			if got != tt.want {
				t.Errorf("parseWpaCliSSID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMonitor_State_EthernetWithWifiAvailable(t *testing.T) {
	// When ethernet is primary but wifi hardware exists, wifi status should be populated
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "eth0", Index: 2, Up: true, Running: true},
		{Name: "wlan0", Index: 3, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
		3: {{LinkIndex: 3, IP: net.ParseIP("192.168.1.51"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")}, // Default route via eth0
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Type != TypeEthernet {
		t.Errorf("State().Type = %q, want %q", state.Type, TypeEthernet)
	}
	// Wifi should show as connected since wlan0 has an IP
	if state.WifiStatus != WifiStatusConnected {
		t.Errorf("State().WifiStatus = %q, want %q", state.WifiStatus, WifiStatusConnected)
	}
}

func TestMonitor_State_Connecting(t *testing.T) {
	// Interface is up and running but has no IP yet
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "wlan0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {}, // No addresses yet
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(WithNetlinkProvider(mock), WithPollInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Status != StatusConnecting {
		t.Errorf("State().Status = %q, want %q", state.Status, StatusConnecting)
	}
}

func TestMonitor_State_Host(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		err      error
		want     string
	}{
		{"plain", "foobar", nil, "foobar"},
		{"trims whitespace", "foobar\n", nil, "foobar"},
		{"fqdn kept", "piz2c.local", nil, "piz2c.local"},
		{"kernel placeholder", "(none)", nil, ""},
		{"empty", "", nil, ""},
		{"whitespace only", "   ", nil, ""},
		{"lookup error", "", errors.New("no hostname"), ""},
		{"shell metacharacters", "foo;rm -rf /", nil, ""},
		{"embedded newline", "foo\nBAR=baz", nil, ""},
		{"quotes", `foo"bar`, nil, ""},
		{"spaces", "my host", nil, ""},
		{"too long", strings.Repeat("a", 254), nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockNetlinkProvider()
			mock.links = []LinkInfo{
				{Name: "lo", Index: 1, Up: true, Running: true},
				{Name: "eth0", Index: 2, Up: true, Running: true},
			}
			mock.addrs = map[int][]AddrInfo{
				2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
			}
			mock.routes = []RouteInfo{
				{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
			}

			m := NewMonitor(
				WithNetlinkProvider(mock),
				WithPollInterval(time.Hour),
				WithLogger(log.New(io.Discard, "", 0)),
				WithHostnameFunc(func() (string, error) { return tt.hostname, tt.err }),
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := m.Start(ctx); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			defer m.Stop()

			if got := m.State().Host; got != tt.want {
				t.Errorf("State().Host = %q, want %q", got, tt.want)
			}
		})
	}
}

// A rejected hostname must not take the rest of the state down with it.
func TestMonitor_State_HostFailureKeepsNetworkState(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{
		{Name: "lo", Index: 1, Up: true, Running: true},
		{Name: "eth0", Index: 2, Up: true, Running: true},
	}
	mock.addrs = map[int][]AddrInfo{
		2: {{LinkIndex: 2, IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)}},
	}
	mock.routes = []RouteInfo{
		{LinkIndex: 2, Dst: nil, Gateway: net.ParseIP("192.168.1.1")},
	}

	m := NewMonitor(
		WithNetlinkProvider(mock),
		WithPollInterval(time.Hour),
		WithLogger(log.New(io.Discard, "", 0)),
		WithHostnameFunc(func() (string, error) { return "", errors.New("boom") }),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()

	state := m.State()
	if state.Status != StatusConnected {
		t.Errorf("State().Status = %q, want %q", state.Status, StatusConnected)
	}
	if state.IP != "192.168.1.50" {
		t.Errorf("State().IP = %q, want %q", state.IP, "192.168.1.50")
	}
	if state.Host != "" {
		t.Errorf("State().Host = %q, want empty", state.Host)
	}
}

// A failing lookup logs once, not on every poll.
func TestMonitor_Host_WarnsOnce(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{{Name: "lo", Index: 1, Up: true, Running: true}}

	var buf bytes.Buffer
	m := NewMonitor(
		WithNetlinkProvider(mock),
		WithPollInterval(time.Hour),
		WithLogger(log.New(&buf, "", 0)),
		WithHostnameFunc(func() (string, error) { return "", errors.New("boom") }),
	)

	for i := 0; i < 5; i++ {
		m.queryState()
	}

	if got := strings.Count(buf.String(), "failed to get hostname"); got != 1 {
		t.Errorf("logged hostname failure %d times, want 1", got)
	}
}

// Once the hostname comes back, a later failure is reported again.
func TestMonitor_Host_WarnsAgainAfterRecovery(t *testing.T) {
	mock := NewMockNetlinkProvider()
	mock.links = []LinkInfo{{Name: "lo", Index: 1, Up: true, Running: true}}

	var buf bytes.Buffer
	fail := true
	m := NewMonitor(
		WithNetlinkProvider(mock),
		WithPollInterval(time.Hour),
		WithLogger(log.New(&buf, "", 0)),
		WithHostnameFunc(func() (string, error) {
			if fail {
				return "", errors.New("boom")
			}
			return "foobar", nil
		}),
	)

	m.queryState() // fails, logs
	fail = false
	if got := m.queryState().Host; got != "foobar" { // recovers, clears the latch
		t.Fatalf("Host = %q, want %q", got, "foobar")
	}
	fail = true
	m.queryState() // fails again, logs again

	if got := strings.Count(buf.String(), "failed to get hostname"); got != 2 {
		t.Errorf("logged hostname failure %d times, want 2", got)
	}
}
