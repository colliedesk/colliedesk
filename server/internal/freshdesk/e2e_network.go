//go:build e2e

package freshdesk

import "crypto/x509"

// UseE2ENetwork lets an end-to-end build reach a local fake Freshdesk: it
// trusts roots and allows exactly the given loopback addresses. Only binaries
// built with -tags e2e contain it; release builds keep the public-address guard.
func UseE2ENetwork(o *Options, roots *x509.CertPool, addresses ...string) {
	o.hooks.rootCAs = roots
	o.hooks.allowDial = map[string]bool{}
	for _, address := range addresses {
		o.hooks.allowDial[address] = true
	}
}
