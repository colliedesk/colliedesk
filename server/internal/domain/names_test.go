package domain

import (
	"strings"
	"testing"
)

func TestHeaderNameIsTrimmedAndRefusesBreaksAndOverrides(t *testing.T) {
	cases := []struct {
		raw, want string
		ok        bool
	}{
		{"  Colliedesk Support  ", "Colliedesk Support", true},
		{"Équipe d’assistance", "Équipe d’assistance", true},
		{strings.Repeat("é", 100), strings.Repeat("é", 100), true},
		{strings.Repeat("é", 101), "", false},
		{"", "", false},
		{"   ", "", false},
		{"Acme\r\nBcc: victim@example.com", "", false},
		{"Tab\there", "", false},
		{"Spoof ‮gnp.exe", "", false},
	}
	for _, item := range cases {
		got, ok := NormalizeHeaderName(item.raw)
		if got != item.want || ok != item.ok {
			t.Errorf("NormalizeHeaderName(%q) = %q, %v; want %q, %v", item.raw, got, ok, item.want, item.ok)
		}
	}
}

// A new channel starts with the workspace name the way the upgrade gave existing channels theirs.
func TestDefaultSenderNameIsTheWorkspaceNameMadeHeaderSafe(t *testing.T) {
	long := strings.Repeat("Ab", 60)
	cases := []struct{ name, slug, want string }{
		{"HC Labs", "hclabs", "HC Labs"},
		{long, "long", long[:100]},
		{"Acme\r\nBcc: victim@example.com", "acme", "Acme  Bcc: victim@example.com"},
		{"\t\n", "controls", "controls"},
	}
	for _, item := range cases {
		if got := DefaultSenderName(item.name, item.slug); got != item.want {
			t.Errorf("DefaultSenderName(%q) = %q, want %q", item.name, got, item.want)
		}
	}
}
