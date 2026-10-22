package helpcenter

import (
	"context"
	"errors"
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

func TestCheckDomainProvesOwnershipOnlyByTheWorkspacesOwnCNAMEOrToken(t *testing.T) {
	const host, slugHost, token = "help.acme.example", "acme.help.example.net", "0123456789abcdef0123456789abcdef"
	ctx := context.Background()
	for name, c := range map[string]struct {
		dns      stubResolvers
		slugHost string
		proven   bool
	}{
		"CNAME to the slug host":                      {stubResolvers{cname: map[string]string{host: slugHost + "."}}, slugHost, true},
		"CNAME in capitals":                           {stubResolvers{cname: map[string]string{host: "ACME.Help.Example.NET."}}, slugHost, true},
		"CNAME to another workspace":                  {stubResolvers{cname: map[string]string{host: "beta.help.example.net."}}, slugHost, false},
		"CNAME to a name ending like the slug host":   {stubResolvers{cname: map[string]string{host: "xacme.help.example.net."}}, slugHost, false},
		"CNAME to the slug host under another domain": {stubResolvers{cname: map[string]string{host: slugHost + ".evil.example."}}, slugHost, false},
		"only an A record":                            {stubResolvers{cname: map[string]string{host: host + "."}}, slugHost, false},
		"TXT token":                                   {stubResolvers{cname: map[string]string{host: host + "."}, txt: map[string][]string{"_help-center-verify." + host: {"v=spf1 -all", " " + token + " "}}}, slugHost, true},
		"another workspace's TXT token":               {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {"fedcba9876543210fedcba9876543210"}}}, slugHost, false},
		"the token on the host itself":                {stubResolvers{txt: map[string][]string{host: {token}}}, slugHost, false},
		"the token inside a longer value":             {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {"x" + token}}}, slugHost, false},
		"CNAME without a help center domain":          {stubResolvers{cname: map[string]string{host: slugHost + "."}}, "", false},
		"TXT without a help center domain":            {stubResolvers{txt: map[string][]string{"_help-center-verify." + host: {token}}}, "", true},
		"nothing at all":                              {stubResolvers{}, slugHost, false},
	} {
		err := CheckDomain(ctx, c.dns.resolvers(), host, c.slugHost, token)
		if (err == nil) != c.proven {
			t.Errorf("%s: CheckDomain error = %v, proven want %v", name, err, c.proven)
		}
		if err != nil && !errors.Is(err, ErrDomainNotProven) {
			t.Errorf("%s: error %v is not ErrDomainNotProven", name, err)
		}
	}
	err := CheckDomain(ctx, stubResolvers{cname: map[string]string{host: "beta.help.example.net."}}.resolvers(), host, slugHost, token)
	for _, want := range []string{host, "beta.help.example.net", slugHost, "_help-center-verify." + host} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the check's message %q does not name %q", err, want)
		}
	}
	err = CheckDomain(ctx, stubResolvers{}.resolvers(), host, "", token)
	if err == nil || strings.Contains(err.Error(), "CNAME") {
		t.Errorf("without a help center domain the message %q must ask for the TXT record only", err)
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
	err := CheckDomain(ctx, slow, "help.acme.example", "acme.help.example.net", "0123456789abcdef0123456789abcdef")
	if !errors.Is(err, ErrDomainNotProven) || time.Since(started) > time.Second {
		t.Fatalf("a hung resolver: error %v after %s", err, time.Since(started))
	}
}

func TestDomainRecords(t *testing.T) {
	const host, token = "help.acme.example", "0123456789abcdef0123456789abcdef"
	verify := DomainRecord{Type: "TXT", Name: "_help-center-verify." + host, Value: token, Purpose: "verify"}
	for name, c := range map[string]struct {
		origin, domain, slug string
		want                 []DomainRecord
	}{
		"help center domain":    {"https://desk.example.com", "help.example.net", "acme", []DomainRecord{{"CNAME", host, "acme.help.example.net", "route_and_verify"}, verify}},
		"no help center domain": {"https://desk.example.com", "", "acme", []DomainRecord{{"CNAME", host, "desk.example.com", "route"}, verify}},
		"reserved slug":         {"https://desk.example.com", "help.example.net", "www", []DomainRecord{{"CNAME", host, "desk.example.com", "route"}, verify}},
		"IPv4 origin":           {"http://192.0.2.10:8080", "", "acme", []DomainRecord{{"A", host, "192.0.2.10", "route"}, verify}},
		"IPv6 origin":           {"http://[2001:db8::10]:8080", "", "acme", []DomainRecord{{"AAAA", host, "2001:db8::10", "route"}, verify}},
	} {
		got := Records(host, token, c.origin, c.domain, c.slug)
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
