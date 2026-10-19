package mailimage

import "net/http"

// UsesEnvironmentProxy reports whether f's transport would read HTTP_PROXY and friends, which would
// carry the fetch around the dial guard.
func UsesEnvironmentProxy(f *Fetcher) bool {
	return f.client.Transport.(*http.Transport).Proxy != nil
}
