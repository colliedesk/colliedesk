package domain_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func TestTrialEmailKeyCollapsesAliasesOfOneMailbox(t *testing.T) {
	cases := map[string]string{
		"Ana.B+test@gmail.com":       "anab@gmail.com",
		"anab@googlemail.com":        "anab@gmail.com",
		"A.n.a.b+x+y@GoogleMail.com": "anab@gmail.com",
		"Ada+x@Example.com":          "ada@example.com",
		"ada.lovelace@example.com":   "ada.lovelace@example.com", // dots count outside Gmail
		"  ada@example.com ":         "ada@example.com",
		"ada@sub.example.com":        "ada@sub.example.com",
	}
	for input, want := range cases {
		got, err := domain.TrialEmailKey(input)
		if err != nil || got != want {
			t.Errorf("TrialEmailKey(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "no-at-sign", "@example.com", "+tag@example.com", "ada@", "a@b@c.com", "ada@gmail.com extra"} {
		if got, err := domain.TrialEmailKey(bad); err == nil {
			t.Errorf("TrialEmailKey(%q) = %q, want an error", bad, got)
		}
	}
}

func TestDeniedBrandMatchesWholeWordsAndSpelledOutNames(t *testing.T) {
	denied := []string{
		"PayPal Support", "pay pal", "G-O-O-G-L-E", "Apple", "APPLE inc", "my.apple.store", "Microsoft",
		"Chase Bank", "Wells Fargo Team", "bank of america help", "Acme - DocuSign", "paypal_help",
	}
	for _, name := range denied {
		if brand, ok := domain.DeniedBrand(name); !ok {
			t.Errorf("DeniedBrand(%q) allowed the name, want a denied brand (matched %q)", name, brand)
		}
	}
	allowed := []string{
		"Acme Support", "Pineapple", "Pineapple Studio", "Applebee Foods", "Googleplex Labs", "Snapple",
		"Dhlia Flowers", "Office Supplies Co", "Maple Leaf", "Stripes", "Cộng đồng Hợp tác xã",
	}
	for _, name := range allowed {
		if brand, ok := domain.DeniedBrand(name); ok {
			t.Errorf("DeniedBrand(%q) denied the name for %q, want it allowed", name, brand)
		}
	}
}
