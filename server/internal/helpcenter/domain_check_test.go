package helpcenter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCustomHostRules(t *testing.T) {
	const origin, domain = "https://desk.example.com", "help.example.net"
	for name, want := range map[string]string{
		"help.acme.example":       "help.acme.example",
		" Help.Acme.Example. ":    "help.acme.example",
		"hilfe.bücher.example":    "hilfe.xn--bcher-kva.example",
		"docs.example.co.uk":      "docs.example.co.uk",
		"desk.example.com.evil.x": "desk.example.com.evil.x",
		"example.net":             "example.net",
	} {
		got, err := CustomHost(name, origin, domain)
		if err != nil || got != want {
			t.Errorf("CustomHost(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{
		"", "   ", "localhost", "acme", "192.0.2.1", "2001:db8::1", "[2001:db8::1]", "10.0.0", "1.2.3.4.5",
		"desk.example.com", "DESK.example.com.", "help.example.net", "acme.help.example.net", "x.acme.help.example.net",
		strings.Repeat("a.", 126) + "com", "https://help.acme.example", "help.acme.example/a", "help.acme.example:8443",
		"a_b.example.com", "-x.example.com", "bad..example.com", "*.example.com", "help.acme.example\x00",
	} {
		if got, err := CustomHost(name, origin, domain); err == nil {
			t.Errorf("CustomHost(%q) = %q, want an error", name, got)
		}
	}
	// 253 characters is the longest name DNS carries.
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".com"
	if len(long) != 253 {
		t.Fatalf("long name has %d characters", len(long))
	}
	if _, err := CustomHost(long, origin, domain); err != nil {
		t.Errorf("a 253-character name was refused: %v", err)
	}
	// Without HELP_CENTER_DOMAIN only the app's own host is out.
	if _, err := CustomHost("acme.help.example.net", origin, ""); err != nil {
		t.Errorf("a name below an unset help center domain was refused: %v", err)
	}
}

// stubResolvers answers from maps, as the DNS of the host's owner would; a missing name is NXDOMAIN.
type stubResolvers struct {
	cname map[string]string
	txt   map[string][]string
}

func (s stubResolvers) resolvers() Resolvers {
	return Resolvers{
		CNAME: func(_ context.Context, host string) (string, error) {
			if target, ok := s.cname[host]; ok {
				return target, nil
			}
			return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
		TXT: func(_ context.Context, name string) ([]string, error) {
			if values, ok := s.txt[name]; ok {
				return values, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		},
	}
}

// A claim is proven only by a CNAME to its own target hc-<claim>.<domain> or by its TXT token. The
// workspace's slug host proves nothing: a slug can be renamed and taken by another workspace.
func TestCheckDomainProvesOwnershipOnlyByTheClaimsOwnCNAMEOrToken(t *testing.T) {
	const host, domain, claim, token = "help.acme.example", "help.example.net", "0123456789abcdef01234567", "0123456789abcdef0123456789abcdef"
	const target = "hc-" + claim + "." + domain
	ctx := context.Background()
	for name, c := range map[string]struct {
		dns    stubResolvers
		domain string
		proven bool
	}{
		"CNAME to the claim's target":              {stubResolvers{cname: map[string]string{host: target + "."}}, domain, true},
		"CNAME in capitals":                        {stubResolvers{cname: map[string]string{host: "HC-0123456789ABCDEF01234567.Help.Example.NET."}}, domain, true},
		"CNAME to the workspace's slug host":       {stubResolvers{cname: map[string]string{host: "acme.help.example.net."}}, domain, false},
		"CNAME to another claim's target":          {stubResolvers{cname: map[string]string{host: "hc-fedcba9876543210fedcba98.help.example.net."}}, domain, false},
		"CNAME to a name ending like the target":   {stubResolvers{cname: map[string]string{host: "x" + target + "."}}, domain, false},
		"CNAME to the target under another domain": {stubResolvers{cname: map[string]string{host: target + ".evil.example."}}, domain, false},
		"only an A record":                         {stubResolvers{cname: map[string]string{host: host + "."}}, domain, false},
		"TXT token":                                {stubResolvers{cname: map[string]string{host: host + "."}, txt: map[string][]string{"_help-center-verify." + host: {"v=spf1 -all", " " + token + " "}}}, domain, true},
		"another workspace's TXT token":            {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {"fedcba9876543210fedcba9876543210"}}}, domain, false},
		"the token on the host itself":             {stubResolvers{txt: map[string][]string{host: {token}}}, domain, false},
		"the token inside a longer value":          {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {"x" + token}}}, domain, false},
		"CNAME to the target without a domain":     {stubResolvers{cname: map[string]string{host: target + "."}}, "", false},
		"TXT without a help center domain":         {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {token}}}, "", true},
		"nothing at all":                           {stubResolvers{}, domain, false},
	} {
		evidence := LookupDomain(ctx, c.dns.resolvers(), host, c.domain)
		err := evidence.CheckClaim(claim, token)
		if (err == nil) != c.proven {
			t.Errorf("%s: Check error = %v, proven want %v", name, err, c.proven)
		}
		if err != nil && !errors.Is(err, ErrDomainNotProven) {
			t.Errorf("%s: error %v is not ErrDomainNotProven", name, err)
		}
	}
	// The message says what to set, never what the server's resolver saw: that would let a tenant read
	// the resolver's answers for any name.
	seen := LookupDomain(ctx, stubResolvers{cname: map[string]string{host: "db.internal.example."}}.resolvers(), host, domain)
	err := seen.CheckClaim(claim, token)
	if err == nil || strings.Contains(err.Error(), "db.internal.example") {
		t.Errorf("the message %q names what DNS answered", err)
	}
	for _, want := range []string{host + " points somewhere else", target, "_help-center-verify." + host} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the check's message %q does not say %q", err, want)
		}
	}
	if detail := seen.Detail(); !strings.Contains(detail, "db.internal.example") {
		t.Errorf("the log detail %q does not keep what DNS answered", detail)
	}
	if err := LookupDomain(ctx, stubResolvers{}.resolvers(), host, domain).CheckClaim(claim, token); err == nil || !strings.Contains(err.Error(), "has no CNAME record pointing at "+target) {
		t.Errorf("without any record the message is %q", err)
	}
	if err := LookupDomain(ctx, stubResolvers{}.resolvers(), host, "").CheckClaim(claim, token); err == nil || strings.Contains(err.Error(), "CNAME") {
		t.Errorf("without a help center domain the message %q must ask for the TXT record only", err)
	}
	// What DNS showed names the claim of the target and the TXT values, for claims of other workspaces.
	both := LookupDomain(ctx, stubResolvers{cname: map[string]string{host: target + "."}, txt: map[string][]string{"_help-center-verify." + host: {" a ", "b"}}}.resolvers(), host, domain)
	if both.Proof.Claim != claim || fmt.Sprint(both.Proof.Tokens) != "[a b]" {
		t.Errorf("proof = %+v", both.Proof)
	}
}

func TestCheckDomainStopsAtItsDeadline(t *testing.T) {
	slow := Resolvers{
		CNAME: func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() },
		TXT:   func(ctx context.Context, _ string) ([]string, error) { <-ctx.Done(); return nil, ctx.Err() },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := LookupDomain(ctx, slow, "help.acme.example", "help.example.net").CheckClaim("0123456789abcdef01234567", "0123456789abcdef0123456789abcdef")
	if !errors.Is(err, ErrDomainNotProven) || !strings.Contains(err.Error(), "DNS did not answer in time") || time.Since(started) > time.Second {
		t.Fatalf("a hung resolver: error %v after %s", err, time.Since(started))
	}
}

func TestDomainRecords(t *testing.T) {
	const host, token, claim = "help.acme.example", "0123456789abcdef0123456789abcdef", "0123456789abcdef01234567"
	verify := DomainRecord{Type: "TXT", Name: "_help-center-verify." + host, Value: token, Purpose: "verify"}
	for name, c := range map[string]struct {
		origin, domain string
		want           []DomainRecord
	}{
		"help center domain":    {"https://desk.example.com", "help.example.net", []DomainRecord{{"CNAME", host, "hc-" + claim + ".help.example.net", "route_and_verify"}, verify}},
		"no help center domain": {"https://desk.example.com", "", []DomainRecord{{"CNAME", host, "desk.example.com", "route"}, verify}},
		"IPv4 origin":           {"http://192.0.2.10:8080", "", []DomainRecord{{"A", host, "192.0.2.10", "route"}, verify}},
		"IPv6 origin":           {"http://[2001:db8::10]:8080", "", []DomainRecord{{"AAAA", host, "2001:db8::10", "route"}, verify}},
	} {
		got := Records(host, claim, token, c.origin, c.domain)
		if len(got) != len(c.want) {
			t.Errorf("%s: Records = %+v, want %+v", name, got, c.want)
			continue
		}
		for index := range got {
			if got[index] != c.want[index] {
				t.Errorf("%s: record %d = %+v, want %+v", name, index, got[index], c.want[index])
			}
		}
	}
}

func TestLiveChecksBoundConcurrencyAndBackOffPerHost(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	checks := NewLiveChecks(2, 30*time.Second, 3, func() time.Time { return now })
	doneA, ok := checks.Begin("a.example")
	if !ok {
		t.Fatal("first check refused")
	}
	doneB, ok := checks.Begin("b.example")
	if !ok {
		t.Fatal("second check refused")
	}
	if _, ok := checks.Begin("c.example"); ok {
		t.Fatal("a third check ran while two were running")
	}
	doneA()
	if _, ok := checks.Begin("a.example"); ok {
		t.Fatal("the same host was checked again within the back-off")
	}
	doneC, ok := checks.Begin("c.example")
	if !ok {
		t.Fatal("a check was refused after a slot freed")
	}
	doneB()
	doneC()
	if _, ok := checks.Begin("d.example"); ok {
		t.Fatal("a fourth host was tracked over the bound while every entry was fresh")
	}
	now = now.Add(31 * time.Second)
	done, ok := checks.Begin("a.example")
	if !ok {
		t.Fatal("the host was still refused after the back-off")
	}
	done()
	if done, ok := checks.Begin("d.example"); !ok {
		t.Fatal("expired entries were not dropped to make room")
	} else {
		done()
	}
}
