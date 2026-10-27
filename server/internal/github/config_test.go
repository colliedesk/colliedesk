package github

import (
	"strings"
	"testing"
)

func TestMissingNamesEverySettingNotSet(t *testing.T) {
	empty := ConfigFromEnv(func(string) string { return " " })
	if empty.Enabled() || strings.Join(empty.Missing(), ",") != "GITHUB_APP_ID,GITHUB_APP_SLUG,GITHUB_APP_PRIVATE_KEY,GITHUB_APP_CLIENT_ID,GITHUB_APP_CLIENT_SECRET,GITHUB_APP_WEBHOOK_SECRET" {
		t.Fatalf("empty env: %v", empty.Missing())
	}
	full := ConfigFromEnv(func(name string) string { return "value-of-" + name })
	if !full.Enabled() || len(full.Missing()) != 0 {
		t.Fatalf("full env: %v", full.Missing())
	}
}
