// Package importkit holds what every importer (Freshdesk, Help Scout) must do the same way: the
// egress guard, the workspace import lock, the trial extension, the storage cap error and the
// handling of images inside message HTML.
package importkit

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ResponseHeaderTimeout bounds the wait for response headers of one request.
const ResponseHeaderTimeout = 60 * time.Second

// ErrBlockedAddress is returned when a dial would reach a non-public address.
var ErrBlockedAddress = errors.New("refusing to connect to a private, loopback or link-local address")

// reservedNetworks are special-purpose ranges that net.IP methods do not
// cover: CGNAT, benchmarking, IETF protocol assignments, the former class E,
// NAT64 (which can embed any IPv4 address, including private ones), 6to4,
// Teredo and documentation prefixes.
var reservedNetworks = func() []*net.IPNet {
	var networks []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "2001:db8::/32"} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}()

// NewTransport dials only public addresses, so a URL or redirect from the source system can never
// reach the host's own network or metadata service. rootCAs and allowDial are test seams: nil and
// empty in every release build.
func NewTransport(rootCAs *x509.CertPool, allowDial map[string]bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: guardAddress(allowDial)}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: ResponseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
		ForceAttemptHTTP2:     true,
	}
}

func guardAddress(allow map[string]bool) func(string, string, syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allow[address] {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
		}
		ip := net.ParseIP(host)
		if ip == nil || BlockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
		}
		return nil
	}
}

// BlockedIP reports whether ip is outside the public internet.
func BlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range reservedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
