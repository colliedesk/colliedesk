package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// OAuth for AI apps (docs/internals/auth.md, "OAuth grants"). The protocol's own addresses live outside
// /api/v1, where every client looks for them; they are mounted by mountOAuth. The consent page is the web
// app's /oauth/authorize, which reads the app with GET /api/v1/oauth/client and allows it with
// POST /api/v1/oauth/authorize (person-only).
const (
	oauthProtectedResourcePath    = "/.well-known/oauth-protected-resource"
	oauthAuthorizationServerPath  = "/.well-known/oauth-authorization-server"
	oauthAuthorizePagePath        = "/oauth/authorize"
	oauthTokenPath                = "/oauth/token"
	oauthRegisterPath             = "/oauth/register"
	oauthClientRoutePath          = "/api/v1/oauth/client"
	oauthAllowRoutePath           = "/api/v1/oauth/authorize"
	mcpPath                       = "/mcp"
	oauthTokenRequestMaxBytes     = 16 << 10
	oauthRegistrationRequestBytes = 64 << 10
)

// OAuthReconnectSteps says how each app connects again; the token endpoint and /mcp answer it when a
// connection no longer works.
const OAuthReconnectSteps = "Connect again: in Claude or ChatGPT, Disconnect the Colliedesk connector, then Connect; " +
	"in Claude Code, run /mcp, choose colliedesk and Authenticate (or claude mcp login colliedesk); " +
	"in Codex, run codex mcp login colliedesk; in Cursor, open Settings → MCP and log in to colliedesk again."

func (a *API) mountOAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET "+oauthProtectedResourcePath, a.protectedResourceMetadata)
	mux.HandleFunc("GET "+oauthProtectedResourcePath+mcpPath, a.protectedResourceMetadata)
	mux.HandleFunc("GET "+oauthAuthorizationServerPath, a.authorizationServerMetadata)
	mux.HandleFunc("GET "+oauthAuthorizationServerPath+mcpPath, a.authorizationServerMetadata)
	mux.HandleFunc("POST "+oauthRegisterPath, a.registerOAuthClient)
	mux.HandleFunc("POST "+oauthTokenPath, a.oauthToken)
	for _, path := range []string{oauthRegisterPath, oauthTokenPath, oauthProtectedResourcePath, oauthProtectedResourcePath + mcpPath, oauthAuthorizationServerPath, oauthAuthorizationServerPath + mcpPath} {
		mux.HandleFunc("OPTIONS "+path, preflightOAuth)
	}
}

// oauthIssuer is the authorization server: APP_ORIGIN, for every workspace (Cloud has one /mcp address).
func (a *API) oauthIssuer() string { return strings.TrimRight(a.config.Origin, "/") }

// mcpResources are the addresses of /mcp a grant for workspaceSlug may be bound to: APP_ORIGIN's, and the
// workspace's own host's where workspaces have one.
func (a *API) mcpResources(workspaceSlug string) []string {
	resources := []string{a.oauthIssuer() + mcpPath}
	if own := strings.TrimRight(a.workspaceOrigin(workspaceSlug), "/") + mcpPath; own != resources[0] {
		resources = append(resources, own)
	}
	return resources
}

// allowCrossOrigin lets a browser-based client (a web IDE) read the protocol's answers; they carry no
// cookie and no session.
func allowCrossOrigin(response http.ResponseWriter) {
	response.Header().Set("Access-Control-Allow-Origin", "*")
	response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
	response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

func preflightOAuth(response http.ResponseWriter, _ *http.Request) {
	allowCrossOrigin(response)
	response.WriteHeader(http.StatusNoContent)
}

// protectedResourceMetadata is RFC 9728 for /mcp on the host the app asked: which authorization server
// signs its tokens.
func (a *API) protectedResourceMetadata(response http.ResponseWriter, request *http.Request) {
	allowCrossOrigin(response)
	writeJSON(response, http.StatusOK, map[string]any{
		"resource":                 strings.TrimRight(a.requestOrigin(request), "/") + mcpPath,
		"authorization_servers":    []string{a.oauthIssuer()},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Colliedesk",
		"resource_documentation":   "https://colliedesk.com/docs/user/mcp",
	})
}

// authorizationServerMetadata is RFC 8414. Its issuer is APP_ORIGIN, so it is served only there: on a
// workspace's own host the metadata would name another origin than the one it came from.
func (a *API) authorizationServerMetadata(response http.ResponseWriter, request *http.Request) {
	allowCrossOrigin(response)
	if _, onWorkspaceHost := workspacehost.Slug(request.Context()); onWorkspaceHost {
		writeError(response, http.StatusNotFound, "not_found", "The authorization server is "+a.oauthIssuer()+".")
		return
	}
	issuer := a.oauthIssuer()
	writeJSON(response, http.StatusOK, map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + oauthAuthorizePagePath,
		"token_endpoint":                                 issuer + oauthTokenPath,
		"registration_endpoint":                          issuer + oauthRegisterPath,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
		"service_documentation":                          "https://colliedesk.com/docs/user/mcp",
	})
}

// writeOAuthError answers in the protocol's shape (RFC 6749 §5.2).
func writeOAuthError(response http.ResponseWriter, status int, code, description string) {
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, status, map[string]string{"error": code, "error_description": description})
}

// registerOAuthClient is dynamic client registration (RFC 7591) for public clients: the answer's client_id
// holds the app's name and redirect addresses, signed; nothing is stored.
func (a *API) registerOAuthClient(response http.ResponseWriter, request *http.Request) {
	allowCrossOrigin(response)
	if allowed, _ := a.oauth.registrations.allow(clientIP(request, a.config.TrustedProxies), a.now()); !allowed {
		response.Header().Set("Retry-After", "3600")
		writeOAuthError(response, http.StatusTooManyRequests, "rate_limited", "Too many app registrations from this address today. Try again later.")
		return
	}
	var input struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	// Apps send more of RFC 7591's fields (logo_uri, scope, software_id…); they are ignored, not refused.
	if err := json.NewDecoder(io.LimitReader(request.Body, oauthRegistrationRequestBytes)).Decode(&input); err != nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_client_metadata", "Send the registration as a JSON object.")
		return
	}
	if method := input.TokenEndpointAuthMethod; method != "" && method != "none" {
		// A confidential client would need a secret this server does not keep; it is registered as public.
		input.TokenEndpointAuthMethod = "none"
	}
	for _, grant := range input.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			writeOAuthError(response, http.StatusBadRequest, "invalid_client_metadata", "Only the authorization_code and refresh_token grant types are supported.")
			return
		}
	}
	redirects, problem := checkRedirectURIs(input.RedirectURIs)
	if problem != "" {
		writeOAuthError(response, http.StatusBadRequest, "invalid_redirect_uri", "The registration "+problem)
		return
	}
	name := clientName(input.ClientName)
	if name == "" {
		name = "AI app"
	}
	clientID, err := a.signClient(request.Context(), registeredClient{Name: name, RedirectURIs: redirects})
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        a.now().Unix(),
		"client_name":                name,
		"redirect_uris":              redirects,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// oauthToken is the token endpoint (RFC 6749 §3.2): form-encoded, public clients, no client secret.
func (a *API) oauthToken(response http.ResponseWriter, request *http.Request) {
	allowCrossOrigin(response)
	request.Body = http.MaxBytesReader(response, request.Body, oauthTokenRequestMaxBytes)
	if err := request.ParseForm(); err != nil || !strings.HasPrefix(request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "Send the token request form-encoded (application/x-www-form-urlencoded).")
		return
	}
	form := request.PostForm
	resource := strings.TrimRight(form.Get("resource"), "/")
	var tokens auth.OAuthTokens
	var err error
	switch form.Get("grant_type") {
	case "authorization_code":
		if form.Get("code") == "" || form.Get("client_id") == "" || form.Get("redirect_uri") == "" || form.Get("code_verifier") == "" {
			writeOAuthError(response, http.StatusBadRequest, "invalid_request", "An authorization_code request needs code, client_id, redirect_uri and code_verifier (PKCE S256).")
			return
		}
		tokens, err = a.auth.ExchangeOAuthCode(request.Context(), auth.OAuthCodeExchange{Code: form.Get("code"), CodeVerifier: form.Get("code_verifier"),
			ClientID: form.Get("client_id"), RedirectURI: form.Get("redirect_uri"), Resource: resource})
	case "refresh_token":
		if form.Get("refresh_token") == "" {
			writeOAuthError(response, http.StatusBadRequest, "invalid_request", "A refresh_token request needs refresh_token.")
			return
		}
		tokens, err = a.auth.RefreshOAuth(request.Context(), auth.OAuthRefresh{RefreshToken: form.Get("refresh_token"), ClientID: form.Get("client_id"), Resource: resource})
	default:
		writeOAuthError(response, http.StatusBadRequest, "unsupported_grant_type", "Use grant_type authorization_code or refresh_token.")
		return
	}
	switch {
	case errors.Is(err, auth.ErrInvalidGrant):
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"This connection to Colliedesk no longer works: it was disconnected in Settings, expired after 30 days without use, or its person was deactivated. "+OAuthReconnectSteps)
		return
	case errors.Is(err, auth.ErrInvalidTarget):
		writeOAuthError(response, http.StatusBadRequest, "invalid_target", "This connection was allowed for another Colliedesk address. "+OAuthReconnectSteps)
		return
	case err != nil:
		writeInternalError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Pragma", "no-cache")
	writeJSON(response, http.StatusOK, map[string]any{
		"access_token":  tokens.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"refresh_token": tokens.RefreshToken,
	})
}

// oauthClientAnswer is what the consent page reads of an app.
type oauthClientAnswer struct {
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	RedirectURI string `json:"redirect_uri"`
	// Host is where the app sends the person back after the decision.
	Host string `json:"host"`
	// Verified: the name comes from a client ID metadata document published at an https address, not
	// from a registration anyone could make.
	Verified bool `json:"verified"`
}

// describeOAuthClient answers the app a consent page shows, and whether redirect_uri is one of its own;
// only then may the page send the browser there (with the code, or error=access_denied on Cancel).
func (a *API) describeOAuthClient(response http.ResponseWriter, request *http.Request) {
	if allowed, _ := a.oauth.lookups.allow(clientIP(request, a.config.TrustedProxies), a.now()); !allowed {
		response.Header().Set("Retry-After", "60")
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many requests from this address. Try again in a minute.")
		return
	}
	query := request.URL.Query()
	client, redirect, err := a.checkOAuthRequest(request, query.Get("client_id"), query.Get("redirect_uri"))
	if err != nil {
		a.writeOAuthClientError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, oauthClientAnswer{ClientID: client.ID, Name: client.Name, RedirectURI: redirect.String(), Host: redirectHost(redirect), Verified: client.Metadata})
}

func (a *API) checkOAuthRequest(request *http.Request, clientID, redirectURI string) (oauthClient, *url.URL, error) {
	if clientID == "" || redirectURI == "" {
		return oauthClient{}, nil, &oauthClientError{"invalid_request", "The app's sign-in address lacks client_id or redirect_uri. Start connecting again from the app."}
	}
	client, err := a.resolveOAuthClient(request.Context(), clientID)
	if err != nil {
		return oauthClient{}, nil, err
	}
	if !client.redirectAllowed(redirectURI) {
		return oauthClient{}, nil, &oauthClientError{"invalid_redirect_uri", "The app asked to return to an address it did not register (" + redirectURI + "). Start connecting again from the app."}
	}
	redirect, err := url.Parse(redirectURI)
	if err != nil {
		return oauthClient{}, nil, &oauthClientError{"invalid_redirect_uri", "The app's return address is not valid."}
	}
	return client, redirect, nil
}

func (a *API) writeOAuthClientError(response http.ResponseWriter, err error) {
	var refused *oauthClientError
	if errors.As(err, &refused) {
		writeError(response, http.StatusBadRequest, refused.Code, refused.Message)
		return
	}
	writeInternalError(response, err)
}

func redirectHost(redirect *url.URL) string {
	if redirect.Host != "" {
		return redirect.Hostname()
	}
	return redirect.Scheme + ":"
}

// allowOAuth is the person's Allow on the consent page: a grant for the app in the workspace of their
// session, and the address that takes its code back to the app. Person-only (routeAccess): a grant
// that made grants would outlive its own disconnection.
func (a *API) allowOAuth(response http.ResponseWriter, request *http.Request) {
	// The cookie path already checked Origin (authorize); the check is repeated for every caller, since
	// this answer carries a credential for another site.
	if request.Header.Get("Origin") != a.requestOrigin(request) {
		writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match this Colliedesk instance.")
		return
	}
	var input struct {
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		ResponseType        string `json:"response_type"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		State               string `json:"state"`
		Resource            string `json:"resource"`
		Scope               string `json:"scope"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the app's sign-in request as JSON.")
		return
	}
	client, redirect, err := a.checkOAuthRequest(request, input.ClientID, input.RedirectURI)
	if err != nil {
		a.writeOAuthClientError(response, err)
		return
	}
	who := principal(request)
	// From here the redirect is trusted: a protocol error goes back to the app, as RFC 6749 §4.1.2.1 says.
	refuse := func(code, description string) {
		writeJSON(response, http.StatusOK, map[string]string{"redirect_to": a.oauthRedirect(redirect, url.Values{"error": {code}, "error_description": {description}, "state": {input.State}})})
	}
	switch {
	case input.ResponseType != "code":
		refuse("unsupported_response_type", "Colliedesk supports response_type=code only.")
		return
	case input.CodeChallengeMethod != "S256" || len(input.CodeChallenge) < 43 || len(input.CodeChallenge) > 128:
		refuse("invalid_request", "Colliedesk requires PKCE with code_challenge_method=S256.")
		return
	}
	resource := strings.TrimRight(input.Resource, "/")
	resources := a.mcpResources(who.Workspace.Slug)
	if resource == "" {
		resource = resources[0]
	}
	valid := false
	for _, allowed := range resources {
		valid = valid || resource == allowed
	}
	if !valid {
		refuse("invalid_target", "Colliedesk issues tokens only for its MCP address "+resources[0]+".")
		return
	}
	code, err := a.auth.AllowOAuth(request.Context(), who, auth.OAuthGrantRequest{ClientID: client.ID, ClientName: client.Name, RedirectURI: input.RedirectURI,
		CodeChallenge: input.CodeChallenge, Resource: resource, UserAgent: request.Header.Get("User-Agent")})
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"redirect_to": a.oauthRedirect(redirect, url.Values{"code": {code}, "state": {input.State}})})
}

// oauthRedirect is the app's redirect address with the answer added to its query, and iss (RFC 9207).
func (a *API) oauthRedirect(redirect *url.URL, values url.Values) string {
	target := *redirect
	query := target.Query()
	for key, value := range values {
		if key == "state" && value[0] == "" {
			continue
		}
		query[key] = value
	}
	query.Set("iss", a.oauthIssuer())
	target.RawQuery = query.Encode()
	return target.String()
}
