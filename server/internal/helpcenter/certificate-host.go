package helpcenter

import (
	"context"
	"net/http"
)

// CertifyHost bounds the database work of the reverse proxy's certificate question for host, a custom
// domain or a claim's CNAME target in lower case, the way Wrap bounds a host lookup. A name the question
// found unclaimed is refused from the cache without a query. A name no cache knows is charged to the
// request's client (Config.AllowLookup) before look runs, and a refused charge is a refusal; a name the
// question found claimed, or the pages found active, is looked up without the charge, so the proxy's
// questions for real domains never wait on a flood of random names. look reports whether the proxy may
// get the certificate and whether any workspace claims host, active or pending. A domain write forgets
// the names it changes (ForgetDomain).
func (s *Site) CertifyHost(request *http.Request, host string, look func(ctx context.Context) (open, claimed bool, err error)) (bool, error) {
	if _, unclaimed := s.hosts.unclaimed.get(host); unclaimed {
		return false, nil
	}
	_, _, claimed := s.hosts.claimed.peek(host)
	_, _, known := s.hosts.known.peek(host)
	if !claimed && !known && s.config.AllowLookup != nil {
		if allowed, _ := s.config.AllowLookup(request); !allowed {
			return false, nil
		}
	}
	generation := pairGeneration(s.hosts.claimed, s.hosts.unclaimed)
	open, claimed, err := look(request.Context())
	if err != nil {
		return false, err
	}
	mark := ""
	if claimed {
		mark = "claimed"
	}
	rememberIn(s.hosts.claimed, s.hosts.unclaimed, host, mark, generation)
	return open, nil
}
