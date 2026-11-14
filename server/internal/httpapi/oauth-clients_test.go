package httpapi

import "testing"

// Only a client ID metadata document on a known AI vendor's host has its name taken as verified: the host
// itself, or a real subdomain of it, never a look-alike.
func TestTrustedClientHostMatchesOnlyTheVendorsHostsAndTheirSubdomains(t *testing.T) {
	for clientID, want := range map[string]bool{
		"https://claude.ai/oauth/claude-code-client-metadata": true,
		"https://api.claude.ai/client.json":                   true,
		"https://chatgpt.com/oauth/codex/client.json":         true,
		"https://cursor.com/oauth/client.json":                true,
		"https://evil-claude.ai/x":                            false,
		"https://claude.ai.evil.com/x":                        false,
		"https://claude.ai@evil.com/x":                        false,
		"https://CLAUDE.AI/x":                                 false,
		"https://claude.ai:8443/x":                            false,
		"https://evil.example/x":                              false,
		"https://xclaude.ai/x":                                false,
	} {
		if got := trustedClientHost(clientID); got != want {
			t.Errorf("trustedClientHost(%q)=%v, want %v", clientID, got, want)
		}
	}
}
