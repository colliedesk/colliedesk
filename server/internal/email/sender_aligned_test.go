package email_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// A sender is verified only by the trusted server's own topmost stamp, and only for the From domain:
// DMARC pass for that domain, or a DKIM pass whose signing domain is that domain.
func TestSenderAlignedNeedsTheTrustedStampToVouchForTheFromDomain(t *testing.T) {
	cases := []struct {
		name    string
		from    string
		trusted string
		headers []string
		want    bool
	}{
		{"dmarc pass for the From domain", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; spf=pass smtp.mailfrom=team.example; dmarc=pass (p=none) header.from=team.example"}, true},
		{"dkim pass signed by the From domain", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dkim=pass header.d=team.example header.s=s1; spf=fail"}, true},
		{"domains compare without case", "ann@team.example", "MX.example.net",
			[]string{"mx.example.net; dkim=pass header.d=Team.Example"}, true},
		{"dkim pass for another domain", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dkim=pass header.d=mailer.example; dmarc=none"}, false},
		{"dkim pass for a parent of the From domain", "ann@eu.team.example", "mx.example.net",
			[]string{"mx.example.net; dkim=pass header.d=team.example"}, false},
		{"dmarc pass for another From domain", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dmarc=pass header.from=other.example"}, false},
		{"dmarc pass without the domain it checked", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dmarc=pass"}, false},
		{"spf pass alone", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; spf=pass smtp.mailfrom=team.example"}, false},
		{"dmarc fail with an aligned dkim pass", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dkim=pass header.d=team.example; dmarc=fail header.from=team.example"}, false},
		{"no trusted authserv-id configured", "ann@team.example", "",
			[]string{"mx.example.net; dmarc=pass header.from=team.example"}, false},
		{"only an untrusted server's stamp", "ann@team.example", "mx.example.net",
			[]string{"mx.other.example; dmarc=pass header.from=team.example"}, false},
		{"the sender's own header below the trusted fail", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dmarc=fail header.from=team.example", "mx.example.net; dmarc=pass header.from=team.example"}, false},
		{"a later hop's stamp above the trusted one", "ann@team.example", "mx.example.net",
			[]string{"relay.example; dmarc=none", "mx.example.net; dmarc=pass header.from=team.example"}, true},
		{"a pass hidden in a comment", "ann@team.example", "mx.example.net",
			[]string{"mx.example.net; dkim=fail (dkim=pass header.d=team.example) header.d=team.example"}, false},
	}
	for _, tc := range cases {
		raw := "From: " + tc.from + "\r\n"
		for _, header := range tc.headers {
			raw += "Authentication-Results: " + header + "\r\n"
		}
		raw += "Subject: x\r\n\r\nbody"
		parsed, err := email.Parse([]byte(raw), tc.trusted)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if parsed.SenderAligned != tc.want {
			t.Errorf("%s: SenderAligned=%v, want %v", tc.name, parsed.SenderAligned, tc.want)
		}
	}
}
