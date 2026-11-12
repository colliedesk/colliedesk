package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const invitePreviewPath = "/api/v1/auth/invite-preview"

type invitePreview struct {
	State         string    `json:"state"`
	WorkspaceName string    `json:"workspace_name"`
	InvitedBy     string    `json:"invited_by"`
	Email         string    `json:"email"`
	Roles         []string  `json:"roles"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// previewFrom asks what an invite link is, from the client address client, on host.
func previewFrom(t *testing.T, site http.Handler, host, client, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, invitePreviewPath, map[string]any{"token": token}, "", nil)
	req.RemoteAddr, req.Host = client+":4242", host
	return serve(site, req)
}

func redeemFrom(t *testing.T, site http.Handler, host, client, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": token}, "", nil)
	req.RemoteAddr, req.Host = client+":4242", host
	return serve(site, req)
}

// sameRefusal reports whether two answers refuse alike: the status, the error code and the message (the
// request id differs by request).
func sameRefusal(t *testing.T, got, want *httptest.ResponseRecorder) bool {
	t.Helper()
	type refusal struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var a, b refusal
	if got.Code != want.Code || json.Unmarshal(got.Body.Bytes(), &a) != nil || json.Unmarshal(want.Body.Bytes(), &b) != nil {
		return false
	}
	return a == b && a.Error.Code != ""
}

func wantPreview(t *testing.T, response *httptest.ResponseRecorder, want invitePreview) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s, want 200", response.Code, response.Body.String())
	}
	var got invitePreview
	decode(t, response, &got)
	if got.State != want.State || got.WorkspaceName != want.WorkspaceName || got.InvitedBy != want.InvitedBy || got.Email != want.Email ||
		fmt.Sprint(got.Roles) != fmt.Sprint(want.Roles) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("preview=%+v, want %+v", got, want)
	}
}

// clockedAPI is an API on a clock the test moves; session makes a workspace and signs its admin in.
func clockedAPI(t *testing.T, configure func(*httpapi.Config)) (http.Handler, *testClock, func(workspace, email string) string, *auth.Service) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 11, 12, 7, 30, 0, 0, time.UTC)}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Now = clock.Now
		if configure != nil {
			configure(config)
		}
	})
	service = service.WithClock(clock.Now)
	return api, clock, func(workspace, email string) string { return createSession(t, api, service, workspace, email) }, service
}

// An invited person sees which workspace they join, who invited them, with which roles and until when,
// before they use the link; looking never uses it up, and the link still signs them in afterwards.
func TestInvitePreviewShowsTheInviteAndLeavesTheLinkUsable(t *testing.T) {
	api, clock, session, _ := clockedAPI(t, nil)
	admin := session("Northwind Support", "mai@northwind.example.com")
	kbCall(t, api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Mai Tran"}, admin, http.StatusOK, nil)
	_, link := createContractInvite(t, api, admin, "Paul@DropTrack.example.com", "agent")
	token := linkToken(t, link)
	pending := invitePreview{State: "pending", WorkspaceName: "Northwind Support", InvitedBy: "Mai Tran", Email: "paul@droptrack.example.com",
		Roles: []string{"Agent"}, ExpiresAt: clock.Now().Add(15 * time.Minute)}

	for range 3 {
		response := previewFrom(t, api, testOriginHost, "192.0.2.10", token)
		validateContractResponse(t, newRequest(t, http.MethodPost, invitePreviewPath, map[string]any{"token": token}, "", nil), response)
		wantPreview(t, response, pending)
	}
	if response := redeemFrom(t, api, testOriginHost, "192.0.2.10", token); response.Code != http.StatusOK {
		t.Fatalf("redeem after three previews status=%d body=%s, want 200", response.Code, response.Body.String())
	}
	used := pending
	used.State = "used"
	wantPreview(t, previewFrom(t, api, testOriginHost, "192.0.2.10", token), used)
}

// A link that stopped working says which of the three happened; a revoked link that also expired says
// it was revoked, and a used link stays used whatever came after.
func TestInvitePreviewSaysWhyAnInviteStoppedWorking(t *testing.T) {
	api, clock, session, _ := clockedAPI(t, nil)
	admin := session("Reasons Desk", "admin@reasons.example.com")
	inviteID, revokedLink := createContractInvite(t, api, admin, "revoked@reasons.example.com", "agent")
	_, expiredLink := createContractInvite(t, api, admin, "expired@reasons.example.com", "admin")
	expires := clock.Now().Add(15 * time.Minute)
	kbCall(t, api, http.MethodDelete, "/api/v1/auth/invites/"+inviteID, nil, admin, http.StatusNoContent, nil)

	wantPreview(t, previewFrom(t, api, testOriginHost, "192.0.2.11", linkToken(t, revokedLink)), invitePreview{
		State: "revoked", WorkspaceName: "Reasons Desk", InvitedBy: "admin@reasons.example.com", Email: "revoked@reasons.example.com", Roles: []string{"Agent"}, ExpiresAt: expires})
	wantPreview(t, previewFrom(t, api, testOriginHost, "192.0.2.11", linkToken(t, expiredLink)), invitePreview{
		State: "pending", WorkspaceName: "Reasons Desk", InvitedBy: "admin@reasons.example.com", Email: "expired@reasons.example.com", Roles: []string{"Admin"}, ExpiresAt: expires})

	clock.set(expires)
	wantPreview(t, previewFrom(t, api, testOriginHost, "192.0.2.11", linkToken(t, expiredLink)), invitePreview{
		State: "expired", WorkspaceName: "Reasons Desk", InvitedBy: "admin@reasons.example.com", Email: "expired@reasons.example.com", Roles: []string{"Admin"}, ExpiresAt: expires})
	wantPreview(t, previewFrom(t, api, testOriginHost, "192.0.2.11", linkToken(t, revokedLink)), invitePreview{
		State: "revoked", WorkspaceName: "Reasons Desk", InvitedBy: "admin@reasons.example.com", Email: "revoked@reasons.example.com", Roles: []string{"Agent"}, ExpiresAt: expires})
}

// A token that is no invite (unknown, mistyped, a pairing link) gets the answer an invalid link gets from
// redeem, so the preview tells nobody whether a token exists; the pairing link still signs in afterwards.
func TestInvitePreviewOfAnythingButAnInviteAnswersAsAnInvalidLink(t *testing.T) {
	api, service := testAPI(t)
	_, pairingURL, err := service.CreateWorkspace(t.Context(), "Pairing Desk", "admin@pairing-preview.example.com")
	if err != nil {
		t.Fatal(err)
	}
	invalid := redeemFrom(t, api, testOriginHost, "192.0.2.12", "no-such-token")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("redeem of an unknown token status=%d", invalid.Code)
	}
	for name, token := range map[string]string{"unknown": "no-such-token", "pairing": linkToken(t, pairingURL), "mistyped": linkToken(t, pairingURL)[:20] + "xxxx"} {
		response := previewFrom(t, api, testOriginHost, "192.0.2.12", token)
		if !sameRefusal(t, response, invalid) {
			t.Errorf("preview of %s token status=%d body=%s, want redeem's %d %s", name, response.Code, response.Body.String(), invalid.Code, invalid.Body.String())
		}
	}
	if response := previewFrom(t, api, testOriginHost, "192.0.2.12", ""); response.Code != http.StatusBadRequest {
		t.Errorf("preview without a token status=%d, want 400", response.Code)
	}
	if response := redeemFrom(t, api, testOriginHost, "192.0.2.12", linkToken(t, pairingURL)); response.Code != http.StatusOK {
		t.Fatalf("pairing link after its preview status=%d body=%s", response.Code, response.Body.String())
	}
}

// On a workspace's own host only that workspace's invites are described: another workspace's invite gets
// the invalid link's answer, shows none of that workspace, and stays usable on its own host.
func TestInvitePreviewOnAWorkspaceHostDescribesOnlyItsOwnInvites(t *testing.T) {
	api, clock, session, _ := clockedAPI(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	alpha := session("Alpha Preview", "admin@alpha-preview.example.com")
	beta := session("Beta Preview", "admin@beta-preview.example.com")
	alphaHost, betaHost := workspaceSlug(t, api, alpha)+".example.net", workspaceSlug(t, api, beta)+".example.net"
	_, link := createContractInvite(t, api, alpha, "new@alpha-preview.example.com", "agent")
	token := linkToken(t, link)

	invalid := redeemFrom(t, site, betaHost, "192.0.2.13", "no-such-token")
	onBeta := previewFrom(t, site, betaHost, "192.0.2.13", token)
	if !sameRefusal(t, onBeta, invalid) {
		t.Fatalf("alpha's invite previewed on beta's host status=%d body=%s, want %d %s", onBeta.Code, onBeta.Body.String(), invalid.Code, invalid.Body.String())
	}
	wantPreview(t, previewFrom(t, site, alphaHost, "192.0.2.13", token), invitePreview{State: "pending", WorkspaceName: "Alpha Preview",
		InvitedBy: "admin@alpha-preview.example.com", Email: "new@alpha-preview.example.com", Roles: []string{"Agent"}, ExpiresAt: clock.Now().Add(15 * time.Minute)})
	if code := redeemOn(t, site, alphaHost, token); code != http.StatusOK {
		t.Fatalf("alpha's invite on its own host after the previews status=%d", code)
	}
}

// A client address that tries many tokens no link has is stopped on both the preview and redeem for a
// minute. Real links never count: an invite previewed again and again, or a pairing link the page asks
// about before its person continues (the page cannot tell the two apart). Another address is not stopped.
func TestInvitePreviewAndRedeemShareOneLimitOnUnknownTokens(t *testing.T) {
	api, clock, session, service := clockedAPI(t, nil)
	admin := session("Limit Desk", "admin@limit-preview.example.com")
	_, link := createContractInvite(t, api, admin, "new@limit-preview.example.com", "agent")
	token := linkToken(t, link)
	_, pairingURL, err := service.CreateWorkspace(t.Context(), "Limit Pairing", "admin@limit-pairing.example.com")
	if err != nil {
		t.Fatal(err)
	}

	for range 40 {
		if response := previewFrom(t, api, testOriginHost, "192.0.2.14", token); response.Code != http.StatusOK {
			t.Fatalf("a working link previewed again status=%d body=%s", response.Code, response.Body.String())
		}
		if response := previewFrom(t, api, testOriginHost, "192.0.2.14", linkToken(t, pairingURL)); response.Code != http.StatusUnauthorized {
			t.Fatalf("a pairing link previewed status=%d body=%s, want 401", response.Code, response.Body.String())
		}
	}
	for try := range 30 {
		guess := fmt.Sprintf("guess-%d", try)
		send := previewFrom
		if try%2 == 1 {
			send = redeemFrom
		}
		response := send(t, api, testOriginHost, "192.0.2.14", guess)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("refused try %d status=%d body=%s, want 401", try, response.Code, response.Body.String())
		}
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"preview": previewFrom(t, api, testOriginHost, "192.0.2.14", token),
		"redeem":  redeemFrom(t, api, testOriginHost, "192.0.2.14", token),
	} {
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
			t.Errorf("%s after 30 refused links status=%d retry-after=%q body=%s, want 429", name, response.Code, response.Header().Get("Retry-After"), response.Body.String())
		}
	}
	if response := previewFrom(t, api, testOriginHost, "192.0.2.15", token); response.Code != http.StatusOK {
		t.Fatalf("another address status=%d, want 200", response.Code)
	}
	clock.set(clock.Now().Add(time.Minute))
	if response := redeemFrom(t, api, testOriginHost, "192.0.2.14", token); response.Code != http.StatusOK {
		t.Fatalf("redeem a minute later status=%d body=%s, want 200", response.Code, response.Body.String())
	}
}
