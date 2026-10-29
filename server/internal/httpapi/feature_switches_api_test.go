package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

type publicFeatures struct {
	Features map[string]struct {
		Enabled bool `json:"enabled"`
	} `json:"features"`
	Billing struct {
		Enabled     bool `json:"enabled"`
		SwitchedOff bool `json:"switched_off"`
	} `json:"billing"`
	CloudSignIn struct {
		Enabled        bool    `json:"enabled"`
		PublishableKey *string `json:"publishable_key"`
	} `json:"cloud_sign_in"`
	Signup struct {
		Enabled bool `json:"enabled"`
	} `json:"signup"`
	Autofix struct {
		Available       bool     `json:"available"`
		MissingSettings []string `json:"missing_settings"`
		OffReason       *string  `json:"off_reason"`
	} `json:"autofix"`
}

func publicConfigOf(t *testing.T, api http.Handler) publicFeatures {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/v1/config", nil, "", nil)
	response := serve(api, req)
	validateContractResponse(t, req, response)
	var config publicFeatures
	decode(t, response, &config)
	return config
}

func featureOff(t *testing.T, response interface {
	Result() *http.Response
}, body string) {
	t.Helper()
	var parsed struct{ Error struct{ Code string } }
	_ = json.Unmarshal([]byte(body), &parsed)
	if response.Result().StatusCode != http.StatusConflict || parsed.Error.Code != "feature_off" {
		t.Fatalf("answer = %d %s, want 409 feature_off", response.Result().StatusCode, body)
	}
}

// Gmail switched off: the app hides it, connecting is refused with the reason, and disconnecting still works.
func TestASwitchedOffMailboxProviderIsHiddenAndRefused(t *testing.T) {
	desk := newGmailDesk(t, "Gmail Off")
	if config := publicConfigOf(t, desk.api); !config.Features["gmail"].Enabled {
		t.Fatalf("gmail with its settings and no switch: %+v", config.Features)
	}
	keys, err := tokenseal.ParseKeys("k1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=")
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Google = google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys}
		config.SwitchedOff = map[string]bool{serverconfig.FeatureGmail: true}
	})
	if config := publicConfigOf(t, api); config.Features["gmail"].Enabled {
		t.Fatalf("gmail switched off reads as on: %+v", config.Features)
	}
	admin := createSession(t, api, service, "Gmail Switched", "admin@gmail-switched.example.com")
	connectEmail(t, api, admin, "support@gmail-switched.example.com")
	view := gmailDesk{api: api, admin: admin}.settings(t, admin)
	if view.Gmail.Available || len(view.Gmail.MissingSettings) != 0 || view.Gmail.OffReason == nil || *view.Gmail.OffReason != "switched_off" {
		t.Fatalf("gmail switched off = %+v, want unavailable and no setting named", view.Gmail)
	}
	connect := request(t, api, http.MethodPost, "/api/v1/channels/email/gmail", nil, admin, nil)
	featureOff(t, connect, connect.Body.String())
	if strings.Contains(connect.Body.String(), "GOOGLE") {
		t.Fatalf("the refusal names a setting: %s", connect.Body.String())
	}
	if disconnect := request(t, api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, admin, nil); disconnect.Code == http.StatusConflict {
		t.Fatalf("disconnecting Gmail while it is off: %d %s", disconnect.Code, disconnect.Body.String())
	}
	// An AI agent hears the same at once: no request is filed for a person to approve into a refusal.
	token := issueToken(t, api, admin, "agent", "admin")
	asked := request(t, api, http.MethodPost, "/api/v1/channels/email/gmail", nil, token.Token, nil)
	featureOff(t, asked, asked.Body.String())
	if filed := queryString(t, pool, `SELECT count(*)::text FROM agent_requests`); filed != "0" {
		t.Fatalf("a request was filed for a feature that is off: %s", filed)
	}
}

// Autofix switched off: hidden from the app (without naming settings), and its people's routes refused.
func TestASwitchedOffAutofixIsHiddenAndRefused(t *testing.T) {
	desk := newGitHubDesk(t)
	if config := publicConfigOf(t, desk.api); !config.Autofix.Available || !config.Features["autofix"].Enabled {
		t.Fatalf("autofix with its settings: %+v", config.Autofix)
	}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.GitHub = desk.fake.Config()
		config.SwitchedOff = map[string]bool{serverconfig.FeatureAutofix: true}
	})
	config := publicConfigOf(t, api)
	if config.Autofix.Available || len(config.Autofix.MissingSettings) != 0 || config.Autofix.OffReason == nil || *config.Autofix.OffReason != "switched_off" || config.Features["autofix"].Enabled {
		t.Fatalf("autofix switched off = %+v", config.Autofix)
	}
	admin := createSession(t, api, service, "Autofix Off", "admin@autofix-off.example.com")
	install := request(t, api, http.MethodPost, "/api/v1/autofix/github/installations", map[string]any{}, admin, nil)
	featureOff(t, install, install.Body.String())
	if settings := request(t, api, http.MethodGet, "/api/v1/autofix/settings", nil, admin, nil); settings.Code != http.StatusOK {
		t.Fatalf("what autofix holds stays readable: %d %s", settings.Code, settings.Body.String())
	}
	// What was sent goes on: a fix can still be cancelled, marked deployed or given its pull request.
	for _, path := range []string{"/api/v1/autofix/fixes/00000000-0000-4000-8000-000000000000/cancel"} {
		if open := request(t, api, http.MethodPost, path, map[string]any{}, admin, nil); open.Code == http.StatusConflict {
			t.Fatalf("%s while autofix is off: %d %s", path, open.Code, open.Body.String())
		}
	}
	if marked := request(t, api, http.MethodPut, "/api/v1/autofix/fixes/00000000-0000-4000-8000-000000000000/deployed", map[string]any{}, admin, nil); marked.Code == http.StatusConflict {
		t.Fatalf("marking a fix deployed while autofix is off: %d %s", marked.Code, marked.Body.String())
	}
	// The ways out stay open: a stored key or a repository can still be removed.
	if removed := request(t, api, http.MethodDelete, "/api/v1/autofix/connectors/cursor_api/key", nil, admin, nil); removed.Code == http.StatusConflict {
		t.Fatalf("removing the Cursor key while autofix is off: %d %s", removed.Code, removed.Body.String())
	}
	if unsigned := request(t, api, http.MethodPost, "/api/v1/webhooks/github", map[string]any{}, "", nil); unsigned.Code == http.StatusConflict || unsigned.Code == http.StatusNotFound {
		t.Fatalf("the GitHub webhook stops while autofix is off: %d", unsigned.Code)
	}
}

// Billing switched off: no workspace is billed or locked, the billing routes say why, and Polar's webhook
// still keeps subscriptions in step.
func TestASwitchedOffBillingKeepsItsWebhook(t *testing.T) {
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Billing = fake.config()
		config.SwitchedOff = map[string]bool{serverconfig.FeatureBilling: true}
	})
	config := publicConfigOf(t, api)
	if config.Billing.Enabled || config.Features["billing"].Enabled || config.Signup.Enabled {
		t.Fatalf("billing switched off = %+v", config)
	}
	admin := createSession(t, api, service, "Billing Off", "admin@billing-off.example.com")
	if !config.BillingSwitchedOff() {
		t.Fatalf("billing switched off does not say so: %+v", config.Billing)
	}
	// What is paid stays visible and cancellable; only buying is refused, and nothing offers it.
	req := newRequest(t, http.MethodGet, "/api/v1/billing", nil, admin, nil)
	billing := serve(api, req)
	validateContractResponse(t, req, billing)
	var plan struct {
		CheckoutPlans      []string `json:"checkout_plans"`
		StorageUpgradePlan *string  `json:"storage_upgrade_plan"`
	}
	decode(t, billing, &plan)
	if billing.Code != http.StatusOK || len(plan.CheckoutPlans) != 0 || plan.StorageUpgradePlan != nil {
		t.Fatalf("the plan while billing is off: %d %s", billing.Code, billing.Body.String())
	}
	checkout := request(t, api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_monthly"}, admin, nil)
	featureOff(t, checkout, checkout.Body.String())
	var founding struct {
		Open bool `json:"open"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/founding", nil, "", nil), &founding)
	if founding.Open {
		t.Fatal("Founding is offered for sale while billing is off")
	}
	if webhook := request(t, api, http.MethodPost, "/api/v1/webhooks/polar", map[string]any{}, "", nil); webhook.Code == http.StatusNotFound || webhook.Code == http.StatusConflict {
		t.Fatalf("Polar's webhook stops while billing is off: %d", webhook.Code)
	}
	// A trial that ended locks nothing while billing is off.
	if _, err := pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at='2020-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if conversations := request(t, api, http.MethodGet, "/api/v1/conversations", nil, admin, nil); conversations.Code != http.StatusOK {
		t.Fatalf("a workspace past its trial while billing is off: %d %s", conversations.Code, conversations.Body.String())
	}
}

// Cloud sign-in switched off: the app offers it no more and the exchange says why, but the operator console
// still opens with a one-time link, so an operator cannot lock themselves out by switching it off.
func TestASwitchedOffCloudSignInLeavesTheOperatorLink(t *testing.T) {
	e := newOperatorEnvWith(t, false, func(serverconfig.Snapshot) error { return nil }, func(config *httpapi.Config) {
		config.CloudAuth = cloudauth.Config{Issuer: "https://identity.example.com", JWKSURL: "https://identity.example.com/jwks", AuthorizedParties: []string{testOrigin}, PublishableKey: "pk_test_example"}
		config.SwitchedOff = map[string]bool{serverconfig.FeatureCloudSignIn: true}
	})
	config := publicConfigOf(t, e.api)
	if config.CloudSignIn.Enabled || config.CloudSignIn.PublishableKey != nil || config.Features["cloud_sign_in"].Enabled {
		t.Fatalf("cloud sign-in switched off = %+v", config.CloudSignIn)
	}
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{}, "a-token", nil)
	req.Header.Set("Origin", testOrigin)
	exchange := serve(e.api, req)
	featureOff(t, exchange, exchange.Body.String())
	if cookie := e.signIn("ops@example.com"); cookie == nil {
		t.Fatal("the operator link does not sign in while cloud sign-in is off")
	}
}

func (c publicFeatures) BillingSwitchedOff() bool { return c.Billing.SwitchedOff && !c.Billing.Enabled }

// Hosted addresses switched off: none is offered to a channel, but a channel already at its hosted address
// keeps seeing it (its forwarding name, its setup), and mail to it is still received.
func TestSwitchedOffHostedAddressesStayForTheChannelsUsingThem(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := newSignupFixture(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.SwitchedOff = map[string]bool{serverconfig.FeatureHostedMail: true}
	})
	response := f.signUp(t, "ada@example.com", "Lovelace Help", defaultPeer)
	wantResponse(t, response, http.StatusCreated, "")
	cookie := sessionCookie(t, response)
	hostedAddress := func() *string {
		var body struct {
			HostedAddress *string `json:"hosted_address"`
		}
		decode(t, request(t, f.api, http.MethodGet, "/api/v1/channels/email", nil, "", cookie), &body)
		return body.HostedAddress
	}
	address := "support@lovelace-help." + hostedDomain
	if offered := hostedAddress(); offered != nil {
		t.Fatalf("a hosted address offered while they are switched off: %s", *offered)
	}
	put := newRequest(t, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, "", cookie)
	put.Header.Set("Origin", testOrigin)
	if saved := serve(f.api, put); saved.Code != http.StatusOK {
		t.Fatalf("support address at the hosted address: %d %s", saved.Code, saved.Body.String())
	}
	if shown := hostedAddress(); shown == nil || *shown != address {
		t.Fatalf("a channel at its hosted address while they are switched off sees %v, want %s", shown, address)
	}
	if result := inboundOK(t, f.api, platformSecret, hostedMail(address, "first@customer.example")); result.Status != "created" {
		t.Fatalf("mail to the hosted address while they are switched off: %+v", result)
	}
	// A new slug renames the hosted address but not the channel's saved one: the channel still uses a hosted
	// address, and keeps seeing it.
	rename := newRequest(t, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "ada-help"}, "", cookie)
	rename.Header.Set("Origin", testOrigin)
	if renamed := serve(f.api, rename); renamed.Code != http.StatusOK {
		t.Fatalf("rename the workspace: %d %s", renamed.Code, renamed.Body.String())
	}
	if shown := hostedAddress(); shown == nil || *shown != "support@ada-help."+hostedDomain {
		t.Fatalf("after a new slug, with hosted addresses switched off, hosted_address = %v", shown)
	}
}
