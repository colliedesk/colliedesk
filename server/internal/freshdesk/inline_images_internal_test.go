package freshdesk

import (
	"net/url"
	"testing"
)

func TestOnlyFreshdeskHostedImagesAreFetched(t *testing.T) {
	base, _ := url.Parse("https://acme.freshdesk.com")
	r := &run{client: &client{base: base}}
	hosted := []string{
		"https://acme.freshdesk.com/inline/attachment?token=abc",
		"https://attachment.freshdesk.com/inline/attachment?token=abc",
		"//euc-attachment.freshdesk.com/x.png",
		"http://acme.freshdesk.com/helpdesk/attachments/9",
		"https://ACME.freshdesk.com/helpdesk/attachments/9",
		"https://s3.amazonaws.com/cdn.freshdesk.com/data/helpdesk/attachments/production/1/original/a.png",
		"https://s3-us-west-2.amazonaws.com/cdn.freshdesk.com/a.png",
		"https://cdn.freshdesk.com.s3.amazonaws.com/a.png",
		"https://cdn.freshdesk.com.s3-eu-west-1.amazonaws.com/a.png",
	}
	for _, src := range hosted {
		if _, ok := r.hostedImageURL(src); !ok {
			t.Errorf("%s should be fetched", src)
		}
	}
	foreign := []string{
		"https://freshdesk.com.evil.example/a.png",
		"https://evilfreshdesk.com/a.png",
		"https://example.org/freshdesk.png",
		"https://s3.amazonaws.com/other-bucket/freshdesk.png",
		"https://169.254.169.254/latest/meta-data",
		"https://s3evil.s3.amazonaws.com/cdn.freshdesk.com/a.png",
		"https://cdn.freshdesk.com.s3-attacker.s3.amazonaws.com/a.png",
		"https://attacker-tenant.freshdesk.com/helpdesk/attachments/1",
		"https://support.freshdesk.com/helpdesk/attachments/1",
		"http://images.example.org/a.png",
		"https://acme.freshdesk.com.evil.example/a.png",
		"data:image/png;base64,AAAA",
		"cid:logo",
		"ftp://acme.freshdesk.com/a.png",
		"/relative/a.png",
	}
	for _, src := range foreign {
		if _, ok := r.hostedImageURL(src); ok {
			t.Errorf("%s must not be fetched", src)
		}
	}
}

func TestImageKeyIgnoresSigningParametersOnly(t *testing.T) {
	parse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	first := imageKey(parse("https://s.freshdesk.com/a.png?Signature=one&Expires=1&X-Amz-Date=x"))
	second := imageKey(parse("https://s.freshdesk.com/a.png?X-Amz-Date=y&Expires=2&Signature=two"))
	if first != second {
		t.Fatal("a new signature must not make a new image")
	}
	if imageKey(parse("https://s.freshdesk.com/inline/attachment?token=a")) == imageKey(parse("https://s.freshdesk.com/inline/attachment?token=b")) {
		t.Fatal("the token identifies the image and must stay in the key")
	}
}
