package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

// An AI app is an OAuth client, known one of two ways and never stored:
//   - Dynamic client registration (RFC 7591): POST /oauth/register returns a client id that is the app's
//     name and redirect addresses, signed with a server secret. Registering stores nothing, so a flood of
//     registrations costs no rows; it is still limited per client address. Anyone can register, so an
//     https redirect must be a known AI app's (checkRegisteredRedirectURIs); another web app proves its
//     host with a client ID metadata document.
//   - A client ID metadata document: the client id is an https address the app publishes its name and
//     redirect addresses at (Claude Code). The server fetches it, guarded against reaching its own
//     network, and keeps it in memory for a while.
const (
	oauthClientPrefix         = "cdk_client_"
	oauthClientKeyName        = "oauth_client_key"
	oauthRegistrationsPerHour = 1000
	oauthClientLookupsPerMin  = 60
	oauthMetadataMaxBytes     = 64 << 10
	oauthMetadataTimeout      = 5 * time.Second
	oauthMetadataCacheFor     = 10 * time.Minute
	oauthMetadataCacheSize    = 1000
	maxOAuthRedirectURIs      = 10
	maxOAuthClientNameRunes   = 80
)

// oauthClient is an app as the consent page and the token endpoint know it.
type oauthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	// Metadata: the id is a client ID metadata document's address; Host is that address's host.
	Metadata bool
	Host     string
}

// oauthClientError is why a client id or a redirect address is refused; the consent page shows it and
// never redirects with it (the redirect is not trusted yet).
type oauthClientError struct{ Code, Message string }

func (e *oauthClientError) Error() string { return e.Message }

// oauthState is the OAuth state a process keeps across configurations (Carry).
type oauthState struct {
	registrations *webFormLimiter
	lookups       *webFormLimiter
	mu            sync.Mutex
	documents     map[string]cachedClient
}

type cachedClient struct {
	client oauthClient
	until  time.Time
}

func newOAuthState() *oauthState {
	return &oauthState{
		registrations: newWindowLimiter(oauthRegistrationsPerHour, webFormMaxKeys, time.Hour),
		lookups:       newWindowLimiter(oauthClientLookupsPerMin, webFormMaxKeys, time.Minute),
		documents:     map[string]cachedClient{},
	}
}

// registeredClient is what a signed client id holds.
type registeredClient struct {
	Name         string   `json:"n"`
	RedirectURIs []string `json:"r"`
}

// signClient makes the client id of a registration.
func (a *API) signClient(ctx context.Context, client registeredClient) (string, error) {
	key, err := a.store.ServerSecret(ctx, oauthClientKeyName)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(client)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return oauthClientPrefix + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// resolveOAuthClient finds the app a client id names: a registration this server signed, or a client
// ID metadata document.
func (a *API) resolveOAuthClient(ctx context.Context, clientID string) (oauthClient, error) {
	if strings.HasPrefix(clientID, oauthClientPrefix) {
		return a.registeredOAuthClient(ctx, clientID)
	}
	if strings.HasPrefix(clientID, "https://") {
		return a.metadataOAuthClient(ctx, clientID)
	}
	return oauthClient{}, &oauthClientError{"invalid_client", "This app is not registered with this Colliedesk server. Remove the server in the app and add it again."}
}

func (a *API) registeredOAuthClient(ctx context.Context, clientID string) (oauthClient, error) {
	refused := &oauthClientError{"invalid_client", "This app's registration was made by another Colliedesk server or has changed. Remove the server in the app and add it again."}
	payload, signature, ok := strings.Cut(strings.TrimPrefix(clientID, oauthClientPrefix), ".")
	if !ok {
		return oauthClient{}, refused
	}
	key, err := a.store.ServerSecret(ctx, oauthClientKeyName)
	if err != nil {
		return oauthClient{}, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(given, mac.Sum(nil)) {
		return oauthClient{}, refused
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return oauthClient{}, refused
	}
	var client registeredClient
	if err := json.Unmarshal(body, &client); err != nil {
		return oauthClient{}, refused
	}
	// A client id signed before https redirects were limited to known AI apps is refused the same way.
	if _, problem := checkRegisteredRedirectURIs(client.RedirectURIs); problem != "" {
		return oauthClient{}, &oauthClientError{"invalid_client", "This app's registration sends you back to a site that is not a known AI app's. Do not allow it; connect the app again from the app itself."}
	}
	return oauthClient{ID: clientID, Name: client.Name, RedirectURIs: client.RedirectURIs}, nil
}

// metadataOAuthClient fetches (or remembers) a client ID metadata document. Only https, a public address
// after resolution on every connection (netguard), no redirect, at most 64 KiB in 5 seconds; the document
// must name itself as client_id and list its redirect addresses.
func (a *API) metadataOAuthClient(ctx context.Context, clientID string) (oauthClient, error) {
	refused := func(message string) error {
		return &oauthClientError{"invalid_client", "The app's client ID metadata document at " + clientID + " " + message}
	}
	address, err := url.Parse(clientID)
	if err != nil || address.Scheme != "https" || address.Host == "" || address.Path == "" || address.Path == "/" ||
		address.User != nil || address.Fragment != "" || len(clientID) > 2048 {
		return oauthClient{}, refused("is not an https address with a path.")
	}
	now := a.now()
	a.oauth.mu.Lock()
	cached, found := a.oauth.documents[clientID]
	a.oauth.mu.Unlock()
	if found && now.Before(cached.until) {
		return cached.client, nil
	}
	fetch, cancel := context.WithTimeout(ctx, oauthMetadataTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(fetch, http.MethodGet, clientID, nil)
	if err != nil {
		return oauthClient{}, refused("could not be read.")
	}
	request.Header.Set("Accept", "application/json")
	response, err := a.oauthMetadataClient().Do(request)
	if err != nil {
		return oauthClient{}, refused("could not be read: " + metadataFetchReason(err) + ".")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return oauthClient{}, refused(fmt.Sprintf("answered %d.", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, oauthMetadataMaxBytes+1))
	if err != nil || len(body) > oauthMetadataMaxBytes {
		return oauthClient{}, refused("is too large or could not be read.")
	}
	var document struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return oauthClient{}, refused("is not JSON.")
	}
	if document.ClientID != clientID {
		return oauthClient{}, refused("names another client_id.")
	}
	redirects, problem := checkRedirectURIs(document.RedirectURIs)
	if problem != "" {
		return oauthClient{}, refused(problem)
	}
	name := clientName(document.ClientName)
	if name == "" {
		name = address.Host
	}
	client := oauthClient{ID: clientID, Name: name, RedirectURIs: redirects, Metadata: true, Host: address.Hostname()}
	a.oauth.mu.Lock()
	if len(a.oauth.documents) >= oauthMetadataCacheSize {
		clear(a.oauth.documents)
	}
	a.oauth.documents[clientID] = cachedClient{client: client, until: now.Add(oauthMetadataCacheFor)}
	a.oauth.mu.Unlock()
	return client, nil
}

func metadataFetchReason(err error) string {
	switch {
	case errors.Is(err, netguard.ErrBlockedAddress):
		return "its host is not a public address"
	case errors.Is(err, context.DeadlineExceeded):
		return "it took longer than 5 seconds"
	}
	return "the connection failed"
}

// oauthMetadataClient is the guarded client of client ID metadata documents; a test sets its own.
func (a *API) oauthMetadataClient() *http.Client {
	if a.config.OAuthMetadataClient != nil {
		return a.config.OAuthMetadataClient
	}
	dialer := &net.Dialer{Timeout: oauthMetadataTimeout, Control: netguard.Control(nil)}
	return &http.Client{
		Timeout:       oauthMetadataTimeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: oauthMetadataTimeout, MaxResponseHeaderBytes: 16 << 10},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// clientName is a name an app gave itself, trimmed to one line of at most 80 characters.
func clientName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > maxOAuthClientNameRunes {
		name = string(runes[:maxOAuthClientNameRunes])
	}
	return name
}

// checkRedirectURIs keeps the redirect addresses an app may use: https anywhere; http only to the
// loopback host (localhost, 127.0.0.1, [::1]), where any port is accepted later (RFC 8252 §7.3); and an
// app's own private scheme with a dot in it (cursor://, vscode://), RFC 8252 §7.1. No fragment.
func checkRedirectURIs(uris []string) ([]string, string) {
	if len(uris) == 0 || len(uris) > maxOAuthRedirectURIs {
		return nil, fmt.Sprintf("must list 1 to %d redirect_uris.", maxOAuthRedirectURIs)
	}
	kept := make([]string, 0, len(uris))
	for _, raw := range uris {
		if problem := redirectProblem(raw); problem != "" {
			return nil, problem
		}
		kept = append(kept, raw)
	}
	return kept, ""
}

// checkRegisteredRedirectURIs is checkRedirectURIs for dynamic registration, which proves no host: an
// https redirect must be on a known AI vendor's host (trustedClientHosts), so a stranger cannot register
// an app that sends a person's code to a site of their own.
func checkRegisteredRedirectURIs(uris []string) ([]string, string) {
	kept, problem := checkRedirectURIs(uris)
	if problem != "" {
		return nil, problem
	}
	for _, raw := range kept {
		address, _ := url.Parse(raw)
		if strings.EqualFold(address.Scheme, "https") && !trustedVendorAddress(address) {
			return nil, "lists an https redirect address that is not a known AI app's (" + raw + "). Publish a client ID metadata document and use its https address as client_id instead."
		}
	}
	return kept, ""
}

func redirectProblem(raw string) string {
	address, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || address.Fragment != "" || address.User != nil {
		return "lists a redirect address that is not valid: " + raw
	}
	switch scheme := strings.ToLower(address.Scheme); {
	case scheme == "https":
		if address.Host == "" {
			return "lists an https redirect address without a host: " + raw
		}
	case scheme == "http":
		if !loopbackHost(address.Hostname()) {
			return "lists an http redirect address that is not on this computer (localhost or 127.0.0.1): " + raw
		}
	case strings.Contains(scheme, ".") || slices.Contains(nativeAppSchemes, scheme):
	default:
		return "lists a redirect address with a scheme this server does not accept: " + raw
	}
	return ""
}

// nativeAppSchemes are private schemes of desktop AI apps without a dot (RFC 8252 asks for one).
var nativeAppSchemes = []string{"cursor", "vscode", "vscode-insiders", "windsurf"}

func loopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// redirectAllowed reports whether requested is one of the app's redirect addresses: exactly, or a
// loopback address that differs only in its port.
func (c oauthClient) redirectAllowed(requested string) bool {
	if slices.Contains(c.RedirectURIs, requested) {
		return true
	}
	asked, err := url.Parse(requested)
	if err != nil || asked.Scheme != "http" || !loopbackHost(asked.Hostname()) || asked.Fragment != "" || asked.User != nil {
		return false
	}
	for _, raw := range c.RedirectURIs {
		registered, err := url.Parse(raw)
		if err != nil || registered.Scheme != "http" || !loopbackHost(registered.Hostname()) {
			continue
		}
		if registered.Hostname() == asked.Hostname() && registered.EscapedPath() == asked.EscapedPath() && registered.RawQuery == asked.RawQuery {
			return true
		}
	}
	return false
}

// trustedClientHosts are the AI vendors whose client ID metadata documents name their own apps: on these
// hosts (or a subdomain) the name an app gives is the vendor's. Any other host published its own name.
var trustedClientHosts = []string{"claude.ai", "anthropic.com", "chatgpt.com", "openai.com", "cursor.com"}

// trustedClientHost reports whether a client ID metadata document's address is on a trusted vendor's
// host: exactly, or a subdomain at a dot boundary, on the default https port, written as browsers do.
func trustedClientHost(clientID string) bool {
	address, err := url.Parse(clientID)
	return err == nil && trustedVendorAddress(address)
}

// trustedVendorAddress reports whether an https address is on a trusted vendor's host, default port.
func trustedVendorAddress(address *url.URL) bool {
	if !strings.EqualFold(address.Scheme, "https") || address.User != nil || address.Port() != "" {
		return false
	}
	host := address.Hostname()
	for _, trusted := range trustedClientHosts {
		if host == trusted || strings.HasSuffix(host, "."+trusted) {
			return true
		}
	}
	return false
}
