package network

import (
	"net"
)

// LinkInfo represents information about a network link.
type LinkInfo struct {
	Name    string
	Index   int
	Up      bool
	Running bool
}

// AddrInfo represents information about a network address.
type AddrInfo struct {
	LinkIndex int
	IP        net.IP
	Mask      net.IPMask
}

// RouteInfo represents information about a network route.
type RouteInfo struct {
	LinkIndex int
	Dst       *net.IPNet
	Gateway   net.IP
}

// NetlinkProvider defines the interface for netlink operations.
// This allows for mocking in tests and providing a stub on non-Linux platforms.
type NetlinkProvider interface {
	// Links returns all network links.
	Links() ([]LinkInfo, error)
	// Addrs returns all addresses for a given link index.
	Addrs(linkIndex int) ([]AddrInfo, error)
	// Routes returns all routes.
	Routes() ([]RouteInfo, error)
	// Subscribe starts listening for netlink events.
	// Returns channels for link, addr, and route updates, plus a done channel to stop.
	Subscribe() (linkUpdates <-chan LinkInfo, addrUpdates <-chan AddrInfo, routeUpdates <-chan RouteInfo, done chan struct{}, err error)
}

// isWifiInterface returns true if the interface name suggests it's a wifi interface.
func isWifiInterface(name string) bool {
	// Common wifi interface naming patterns
	if len(name) >= 4 && name[:4] == "wlan" {
		return true
	}
	if len(name) >= 4 && name[:4] == "wlp3" {
		return true
	}
	if len(name) >= 3 && name[:3] == "wlp" {
		return true
	}
	return false
}

// isEthernetInterface returns true if the interface name suggests it's an ethernet interface.
func isEthernetInterface(name string) bool {
	// Common ethernet interface naming patterns
	if len(name) >= 3 && name[:3] == "eth" {
		return true
	}
	if len(name) >= 3 && name[:3] == "enp" {
		return true
	}
	if len(name) >= 3 && name[:3] == "eno" {
		return true
	}
	if len(name) >= 3 && name[:3] == "ens" {
		return true
	}
	return false
}

// isDefaultRoute returns true if the route is a default route (destination 0.0.0.0/0).
func isDefaultRoute(route RouteInfo) bool {
	return route.Dst == nil || (route.Dst.IP.Equal(net.IPv4zero) && route.Dst.Mask.String() == "00000000")
}
