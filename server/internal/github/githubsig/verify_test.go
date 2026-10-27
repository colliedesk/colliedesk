package githubsig

import (
	"errors"
	"strings"
	"testing"
)

// The example of GitHub's documentation: secret "It's a Secret to Everybody", body "Hello, World!".
func TestTheDocumentedExampleVerifies(t *testing.T) {
	const want = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	body := []byte("Hello, World!")
	if got := Sign("It's a Secret to Everybody", body); got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
	if err := Verify("It's a Secret to Everybody", want, body); err != nil {
		t.Fatal(err)
	}
}

func TestEveryOtherSignatureIsRefused(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	good := Sign("secret", body)
	for name, c := range map[string]struct{ secret, signature string }{
		"missing":       {"secret", ""},
		"no prefix":     {"secret", strings.TrimPrefix(good, "sha256=")},
		"sha1":          {"secret", "sha1=" + strings.TrimPrefix(good, "sha256=")[:40]},
		"not hex":       {"secret", "sha256=zz"},
		"short":         {"secret", good[:20]},
		"other secret":  {"other", good},
		"no secret set": {"", Sign("", body)},
		"other body":    {"secret", Sign("secret", []byte(`{"action":"closed"}`))},
	} {
		if err := Verify(c.secret, c.signature, body); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}
