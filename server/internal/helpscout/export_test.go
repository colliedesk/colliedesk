package helpscout

import "crypto/x509"

// UseTestNetwork trusts the fake servers' certificate, lets the importer dial exactly the given
// loopback addresses and treats them as Help Scout image hosts.
func UseTestNetwork(o *Options, roots *x509.CertPool, addresses ...string) {
	o.hooks.rootCAs = roots
	o.hooks.allowDial, o.hooks.imageHosts = map[string]bool{}, map[string]bool{}
	for _, address := range addresses {
		o.hooks.allowDial[address] = true
		o.hooks.imageHosts[address] = true
	}
}
