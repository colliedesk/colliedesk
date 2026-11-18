package httpapi

import (
	"context"
	"net/url"
	"strings"
)

// signupAppOther is the app of a connector sign-up whose client this server cannot name.
const signupAppOther = "other"

// metadataHostApps names the app of a client ID metadata document by the host that serves it; the address
// is only parsed, never fetched. The sign-up form carries the address, so anyone can make a sign-up name an
// app: the name only counts sign-ups by source and is never trusted for anything else.
var metadataHostApps = []struct{ host, app string }{
	{"claude.ai", "claude"}, {"anthropic.com", "claude"},
	{"chatgpt.com", "chatgpt"}, {"openai.com", "chatgpt"},
	{"cursor.com", "cursor"}, {"glama.ai", "glama"},
}

// metadataPathApps names the vendor's other app whose document sits on the same host under its own path:
// Claude Code on the Claude hosts, Codex on the ChatGPT hosts.
var metadataPathApps = []struct{ hostApp, word, app string }{
	{"claude", "claude-code", "claude_code"}, {"chatgpt", "codex", "codex"},
}

// registeredNameApps names the app of a registration by the name it gave, first match wins: "Claude Code"
// before "Claude". A name is the app's own claim, good enough to count sign-ups, never to trust.
var registeredNameApps = []struct{ word, app string }{
	{"claude code", "claude_code"}, {"codex", "codex"}, {"cursor", "cursor"},
	{"visual studio code", "vscode"}, {"vscode", "vscode"},
	{"chatgpt", "chatgpt"}, {"claude", "claude"}, {"glama", "glama"},
}

// signupApp names the AI app whose connector started a sign-up, from the client_id of the consent page
// it returns to (next): one word of the lists above, or "other". It reads no network and never fails: the
// trial starts either way.
func (a *API) signupApp(ctx context.Context, next string) string {
	target, err := url.Parse(next)
	if err != nil {
		return signupAppOther
	}
	clientID := target.Query().Get("client_id")
	switch {
	case strings.HasPrefix(clientID, oauthClientPrefix):
		client, err := a.registeredOAuthClient(ctx, clientID)
		if err != nil {
			return signupAppOther
		}
		return registeredApp(client.Name)
	case strings.HasPrefix(clientID, "https://"):
		return metadataApp(clientID)
	}
	return signupAppOther
}

func metadataApp(clientID string) string {
	address, err := url.Parse(clientID)
	if err != nil || address.User != nil || address.Port() != "" {
		return signupAppOther
	}
	host := address.Hostname()
	for _, known := range metadataHostApps {
		if host == known.host || strings.HasSuffix(host, "."+known.host) {
			return pathApp(known.app, strings.ToLower(address.Path))
		}
	}
	return signupAppOther
}

func pathApp(hostApp, path string) string {
	for _, known := range metadataPathApps {
		if known.hostApp == hostApp && strings.Contains(path, known.word) {
			return known.app
		}
	}
	return hostApp
}

func registeredApp(name string) string {
	name = strings.ToLower(name)
	for _, known := range registeredNameApps {
		if strings.Contains(name, known.word) {
			return known.app
		}
	}
	return signupAppOther
}
