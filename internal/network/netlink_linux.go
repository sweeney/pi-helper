//go:build linux

package network

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// LinuxNetlinkProvider implements NetlinkProvider using the vishvananda/netlink library.
type LinuxNetlinkProvider struct{}

// NewNetlinkProvider creates a new Linux netlink provider.
func NewNetlinkProvider() NetlinkProvider {
	return &LinuxNetlinkProvider{}
}

// Links returns all network links.
func (p *LinuxNetlinkProvider) Links() ([]LinkInfo, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list links: %w", err)
	}

	result := make([]LinkInfo, 0, len(links))
	for _, link := range links {
		attrs := link.Attrs()
		result = append(result, LinkInfo{
			Name:    attrs.Name,
			Index:   attrs.Index,
			Up:      attrs.RawFlags&0x1 != 0,     // IFF_UP
			Running: attrs.RawFlags&0x10000 != 0, // IFF_LOWER_UP (link layer is up)
		})
	}
	return result, nil
}

// Addrs returns all addresses for a given link index.
func (p *LinuxNetlinkProvider) Addrs(linkIndex int) ([]AddrInfo, error) {
	link, err := netlink.LinkByIndex(linkIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get link by index %d: %w", linkIndex, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list addresses: %w", err)
	}

	result := make([]AddrInfo, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil && addr.IP.To4() != nil {
			result = append(result, AddrInfo{
				LinkIndex: linkIndex,
				IP:        addr.IP,
				Mask:      addr.Mask,
			})
		}
	}
	return result, nil
}

// Routes returns all routes.
func (p *LinuxNetlinkProvider) Routes() ([]RouteInfo, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	result := make([]RouteInfo, 0, len(routes))
	for _, route := range routes {
		result = append(result, RouteInfo{
			LinkIndex: route.LinkIndex,
			Dst:       route.Dst,
			Gateway:   route.Gw,
		})
	}
	return result, nil
}

// Subscribe starts listening for netlink events.
func (p *LinuxNetlinkProvider) Subscribe() (<-chan LinkInfo, <-chan AddrInfo, <-chan RouteInfo, chan struct{}, error) {
	linkCh := make(chan LinkInfo, 10)
	addrCh := make(chan AddrInfo, 10)
	routeCh := make(chan RouteInfo, 10)
	done := make(chan struct{})

	// Subscribe to link updates
	linkUpdateCh := make(chan netlink.LinkUpdate)
	if err := netlink.LinkSubscribe(linkUpdateCh, done); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to subscribe to link updates: %w", err)
	}

	// Subscribe to address updates
	addrUpdateCh := make(chan netlink.AddrUpdate)
	if err := netlink.AddrSubscribe(addrUpdateCh, done); err != nil {
		close(done)
		return nil, nil, nil, nil, fmt.Errorf("failed to subscribe to addr updates: %w", err)
	}

	// Subscribe to route updates
	routeUpdateCh := make(chan netlink.RouteUpdate)
	if err := netlink.RouteSubscribe(routeUpdateCh, done); err != nil {
		close(done)
		return nil, nil, nil, nil, fmt.Errorf("failed to subscribe to route updates: %w", err)
	}

	// Forward link updates
	go func() {
		for update := range linkUpdateCh {
			attrs := update.Link.Attrs()
			linkCh <- LinkInfo{
				Name:    attrs.Name,
				Index:   attrs.Index,
				Up:      attrs.RawFlags&0x1 != 0,     // IFF_UP
				Running: attrs.RawFlags&0x10000 != 0, // IFF_LOWER_UP
			}
		}
		close(linkCh)
	}()

	// Forward address updates
	go func() {
		for update := range addrUpdateCh {
			if update.LinkAddress.IP != nil && update.LinkAddress.IP.To4() != nil {
				addrCh <- AddrInfo{
					LinkIndex: update.LinkIndex,
					IP:        update.LinkAddress.IP,
					Mask:      update.LinkAddress.Mask,
				}
			}
		}
		close(addrCh)
	}()

	// Forward route updates
	go func() {
		for update := range routeUpdateCh {
			routeCh <- RouteInfo{
				LinkIndex: update.Route.LinkIndex,
				Dst:       update.Route.Dst,
				Gateway:   update.Route.Gw,
			}
		}
		close(routeCh)
	}()

	return linkCh, addrCh, routeCh, done, nil
}
