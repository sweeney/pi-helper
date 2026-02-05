//go:build !linux

package network

import (
	"errors"
)

// StubNetlinkProvider is a non-functional netlink provider for non-Linux platforms.
type StubNetlinkProvider struct{}

// NewNetlinkProvider creates a stub netlink provider on non-Linux platforms.
func NewNetlinkProvider() NetlinkProvider {
	return &StubNetlinkProvider{}
}

// ErrNotSupported is returned when netlink operations are attempted on non-Linux platforms.
var ErrNotSupported = errors.New("netlink is not supported on this platform")

// Links returns an error as netlink is not supported.
func (p *StubNetlinkProvider) Links() ([]LinkInfo, error) {
	return nil, ErrNotSupported
}

// Addrs returns an error as netlink is not supported.
func (p *StubNetlinkProvider) Addrs(linkIndex int) ([]AddrInfo, error) {
	return nil, ErrNotSupported
}

// Routes returns an error as netlink is not supported.
func (p *StubNetlinkProvider) Routes() ([]RouteInfo, error) {
	return nil, ErrNotSupported
}

// Subscribe returns an error as netlink is not supported.
func (p *StubNetlinkProvider) Subscribe() (<-chan LinkInfo, <-chan AddrInfo, <-chan RouteInfo, chan struct{}, error) {
	return nil, nil, nil, nil, ErrNotSupported
}
