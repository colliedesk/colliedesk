package freshdesk

import (
	"crypto/x509"
	"time"
)

// Test-only seams: trust the fake servers' certificate, let the importer dial
// exactly the given loopback addresses, and shorten timeouts and probing.

func UseTestNetwork(o *Options, roots *x509.CertPool, addresses ...string) {
	o.hooks.rootCAs = roots
	o.hooks.allowDial = map[string]bool{}
	for _, address := range addresses {
		o.hooks.allowDial[address] = true
	}
}

func SetTestTimeouts(o *Options, api, idle time.Duration) {
	o.hooks.apiTimeout, o.hooks.idleTimeout = api, idle
}

func SetProbeAhead(o *Options, n int) { o.hooks.probeAhead = n }
