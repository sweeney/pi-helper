package network

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HostnameFunc returns the system hostname.
type HostnameFunc func() (string, error)

// hostnameNone is the placeholder the Linux kernel reports when no hostname
// has been set (e.g. before DHCP or systemd-hostnamed has run).
const hostnameNone = "(none)"

// Monitor watches network state and notifies subscribers of changes.
type Monitor struct {
	provider     NetlinkProvider
	pollInterval time.Duration
	verbose      bool
	logger       *log.Logger
	hostname     HostnameFunc

	// hostnameWarned ensures a failing hostname lookup is logged once rather
	// than on every poll. queryState runs from both the netlink and poll
	// goroutines, so this must be atomic.
	hostnameWarned atomic.Bool

	mu          sync.RWMutex
	state       State
	subscribers []chan State

	done chan struct{}
	wg   sync.WaitGroup
}

// MonitorOption is a functional option for configuring the Monitor.
type MonitorOption func(*Monitor)

// WithPollInterval sets the polling interval for the monitor.
func WithPollInterval(d time.Duration) MonitorOption {
	return func(m *Monitor) {
		m.pollInterval = d
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(v bool) MonitorOption {
	return func(m *Monitor) {
		m.verbose = v
	}
}

// WithNetlinkProvider sets a custom netlink provider (for testing).
func WithNetlinkProvider(p NetlinkProvider) MonitorOption {
	return func(m *Monitor) {
		m.provider = p
	}
}

// WithHostnameFunc sets a custom hostname lookup (for testing).
func WithHostnameFunc(f HostnameFunc) MonitorOption {
	return func(m *Monitor) {
		m.hostname = f
	}
}

// WithLogger sets a custom logger.
func WithLogger(l *log.Logger) MonitorOption {
	return func(m *Monitor) {
		m.logger = l
	}
}

// NewMonitor creates a new network monitor.
func NewMonitor(opts ...MonitorOption) *Monitor {
	m := &Monitor{
		provider:     NewNetlinkProvider(),
		pollInterval: 30 * time.Second,
		state:        NewState(),
		done:         make(chan struct{}),
		logger:       log.New(os.Stderr, "[network] ", log.LstdFlags),
		hostname:     os.Hostname,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Start begins monitoring network state.
func (m *Monitor) Start(ctx context.Context) error {
	// Do initial state poll
	m.poll()

	// Try to start netlink subscription
	linkCh, addrCh, routeCh, netlinkDone, err := m.provider.Subscribe()
	if err != nil {
		if m.verbose {
			m.logger.Printf("netlink subscription failed, using polling only: %v", err)
		}
		// Fall back to polling-only mode
		m.wg.Add(1)
		go m.pollLoop(ctx)
		return nil
	}

	// Start event handler
	m.wg.Add(1)
	go m.eventLoop(ctx, linkCh, addrCh, routeCh, netlinkDone)

	// Start polling backup
	m.wg.Add(1)
	go m.pollLoop(ctx)

	return nil
}

// State returns the current network state.
func (m *Monitor) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Subscribe returns a channel that receives state updates.
func (m *Monitor) Subscribe() <-chan State {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan State, 10)
	m.subscribers = append(m.subscribers, ch)
	return ch
}

// Stop stops the monitor and cleans up resources.
func (m *Monitor) Stop() {
	close(m.done)
	m.wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = nil
}

// eventLoop handles netlink events.
func (m *Monitor) eventLoop(ctx context.Context, linkCh <-chan LinkInfo, addrCh <-chan AddrInfo, routeCh <-chan RouteInfo, netlinkDone chan struct{}) {
	defer m.wg.Done()
	defer close(netlinkDone)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		case link, ok := <-linkCh:
			if !ok {
				return
			}
			if m.verbose {
				m.logger.Printf("netlink: link event: %s (up=%v running=%v)", link.Name, link.Up, link.Running)
			}
			m.poll()
		case _, ok := <-addrCh:
			if !ok {
				return
			}
			if m.verbose {
				m.logger.Printf("netlink: address event")
			}
			m.poll()
		case _, ok := <-routeCh:
			if !ok {
				return
			}
			if m.verbose {
				m.logger.Printf("netlink: route event")
			}
			m.poll()
		}
	}
}

// pollLoop periodically polls for network state.
func (m *Monitor) pollLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

// poll queries the current network state and updates if changed.
func (m *Monitor) poll() {
	newState := m.queryState()

	m.mu.Lock()
	changed := !m.state.Equal(newState)
	oldState := m.state
	if changed {
		m.state = newState
	}
	subscribers := m.subscribers
	m.mu.Unlock()

	if changed {
		m.logger.Printf("state changed: %s/%s → %s/%s (wifi: %s → %s, ip: %s → %s)",
			oldState.Status, oldState.Type, newState.Status, newState.Type,
			oldState.WifiStatus, newState.WifiStatus,
			oldState.IP, newState.IP)
	}

	if changed {
		for _, ch := range subscribers {
			select {
			case ch <- newState:
			default:
				// Channel full, skip
			}
		}
	}
}

// queryState queries the current network state from the system.
func (m *Monitor) queryState() State {
	state := NewState()
	state.Host = m.getHostname()

	// Check for wifi hardware
	hasWifi := m.detectWifiHardware()
	if !hasWifi {
		state.WifiStatus = WifiStatusNoHardware
	}

	// Get links, addresses, and routes
	links, err := m.provider.Links()
	if err != nil {
		if m.verbose {
			m.logger.Printf("failed to get links: %v", err)
		}
		return state
	}

	routes, err := m.provider.Routes()
	if err != nil {
		if m.verbose {
			m.logger.Printf("failed to get routes: %v", err)
		}
		return state
	}

	// Find the primary interface (has default route)
	var primaryLink *LinkInfo
	var gateway string

	for _, route := range routes {
		if isDefaultRoute(route) && route.Gateway != nil {
			for i := range links {
				if links[i].Index == route.LinkIndex && links[i].Up && links[i].Running {
					primaryLink = &links[i]
					gateway = route.Gateway.String()
					break
				}
			}
			if primaryLink != nil {
				break
			}
		}
	}

	// If no default route, look for an interface with an IP
	if primaryLink == nil {
		for i := range links {
			link := &links[i]
			if !link.Up || !link.Running {
				continue
			}
			// Skip loopback and virtual interfaces
			if link.Name == "lo" || strings.HasPrefix(link.Name, "docker") || strings.HasPrefix(link.Name, "veth") || strings.HasPrefix(link.Name, "br-") {
				continue
			}

			addrs, err := m.provider.Addrs(link.Index)
			if err != nil {
				continue
			}
			if len(addrs) > 0 {
				primaryLink = link
				break
			}
		}
	}

	if primaryLink == nil {
		// No connected interface found
		state.Status = StatusDisconnected
		return state
	}

	// Get IP address for primary interface
	addrs, err := m.provider.Addrs(primaryLink.Index)
	if err != nil || len(addrs) == 0 {
		state.Status = StatusConnecting
		return state
	}

	state.Status = StatusConnected
	state.IP = addrs[0].IP.String()
	state.Gateway = gateway

	// Determine interface type
	if isWifiInterface(primaryLink.Name) {
		state.Type = TypeWifi
		state.WifiStatus = WifiStatusConnected
		state.WifiSSID = m.getWifiSSID(primaryLink.Name)
	} else if isEthernetInterface(primaryLink.Name) {
		state.Type = TypeEthernet
	}

	// If primary is ethernet but wifi exists, check wifi status separately
	if state.Type == TypeEthernet && hasWifi {
		for _, link := range links {
			if isWifiInterface(link.Name) {
				if link.Up && link.Running {
					// Check if wifi has an IP
					wifiAddrs, err := m.provider.Addrs(link.Index)
					if err == nil && len(wifiAddrs) > 0 {
						state.WifiStatus = WifiStatusConnected
						state.WifiSSID = m.getWifiSSID(link.Name)
					} else {
						state.WifiStatus = WifiStatusDisconnected
					}
				} else {
					state.WifiStatus = WifiStatusDisconnected
				}
				break
			}
		}
	}

	return state
}

// getHostname returns the system hostname, or "" if it cannot be determined
// or is not usable as an env var value.
//
// The lookup itself is os.Hostname, which is a plain syscall wrapper available
// on every platform Go builds for, so this needs no per-arch handling the way
// netlink does. What does vary is what the system reports: an unconfigured Pi
// returns "(none)" or an empty string, and the value ends up in a file that
// other services source as shell, so it is validated before being published.
func (m *Monitor) getHostname() string {
	host, err := m.hostname()
	if err != nil {
		// Log once: this polls every 30s and a broken lookup stays broken.
		if !m.hostnameWarned.Swap(true) {
			m.logger.Printf("failed to get hostname: %v (NETWORK_HOST will be empty)", err)
		}
		return ""
	}

	host = strings.TrimSpace(host)
	if host == "" || host == hostnameNone {
		return ""
	}

	if !isValidHostname(host) {
		if !m.hostnameWarned.Swap(true) {
			m.logger.Printf("hostname %q contains unexpected characters, NETWORK_HOST will be empty", host)
		}
		return ""
	}

	m.hostnameWarned.Store(false)
	return host
}

// isValidHostname reports whether host is safe to publish as an env var value.
// It accepts the RFC 1123 character set (letters, digits, hyphen, dot), which
// excludes everything that would break a consumer running `source` on the env
// file: whitespace, quotes, newlines, and shell metacharacters.
func isValidHostname(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// detectWifiHardware checks if wifi hardware is present.
func (m *Monitor) detectWifiHardware() bool {
	links, err := m.provider.Links()
	if err != nil {
		return false
	}

	for _, link := range links {
		if isWifiInterface(link.Name) {
			return true
		}
	}
	return false
}

// getWifiSSID attempts to get the current wifi SSID.
func (m *Monitor) getWifiSSID(ifname string) string {
	// Try wpa_cli first (most reliable on Raspberry Pi)
	if ssid := m.getSSIDFromWpaCli(ifname); ssid != "" {
		return ssid
	}
	// Fall back to iwgetid if available
	if ssid := m.getSSIDFromIwgetid(ifname); ssid != "" {
		return ssid
	}
	return ""
}

// getSSIDFromWpaCli gets the SSID using wpa_cli.
func (m *Monitor) getSSIDFromWpaCli(ifname string) string {
	cmd := exec.Command("wpa_cli", "-i", ifname, "status")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseWpaCliSSID(string(output))
}

// parseWpaCliSSID extracts the SSID from wpa_cli status output.
func parseWpaCliSSID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ssid=") {
			return strings.TrimPrefix(line, "ssid=")
		}
	}
	return ""
}

// getSSIDFromIwgetid gets the SSID using iwgetid.
func (m *Monitor) getSSIDFromIwgetid(ifname string) string {
	cmd := exec.Command("iwgetid", "-r", ifname)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
