package main

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// cliTestRing is the key ring the CLI tests seal secrets with.
const cliTestRing = "k1:Y29sbGllLWNsaS10ZXN0LWtleS0zMi1ieXRlcy4uLi4="

// configure writes settings into the database of databaseURL the way an operator does, with
// collie config import --stdin --replace: variables by their old names, empty means unset.
func configure(t *testing.T, databaseURL string, values map[string]string) {
	t.Helper()
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	t.Setenv("USER", "tests")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines strings.Builder
	for _, name := range names {
		fmt.Fprintf(&lines, "%s=%s\n", name, values[name])
	}
	previous := stdin
	stdin = strings.NewReader(lines.String())
	defer func() { stdin = previous }()
	var output bytes.Buffer
	if err := run(context.Background(), []string{"config", "import", "--stdin", "--replace"}, &output, databaseURL, trialTestOrigin); err != nil {
		t.Fatalf("collie config import: %v\n%s", err, output.String())
	}
}

// configureUnset removes settings, as collie config unset does.
func configureUnset(t *testing.T, databaseURL string, names ...string) {
	t.Helper()
	t.Setenv("USER", "tests")
	var output bytes.Buffer
	if err := run(context.Background(), append([]string{"config", "unset"}, names...), &output, databaseURL, trialTestOrigin); err != nil {
		t.Fatalf("collie config unset %v: %v", names, err)
	}
}
