package textmask

import "testing"

func TestMaskTakesOutEmailsAndRunsOfSixDigitsOrMore(t *testing.T) {
	for in, want := range map[string]string{
		"write to jane.doe@example.com please":        "write to [email removed] please",
		"Jane＠Example.co.uk wrote":                    "[email removed] wrote",
		"call +1 (555) 123-4567 after noon":           "call [number removed] after noon",
		"my order 123456 never came":                  "my order [number removed] never came",
		"card 4111 1111 1111 1111":                    "card [number removed]",
		"account １２３４５６７":                             "account [number removed]",
		"order 12345 is fine":                         "order 12345 is fine",
		"step 3 of 12 does not say where the menu is": "step 3 of 12 does not say where the menu is",
		"": "",
	} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
		if got := Mask(Mask(in)); got != want {
			t.Errorf("Mask twice of %q = %q, want %q", in, got, want)
		}
	}
}
