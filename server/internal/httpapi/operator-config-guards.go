package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/retention"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// The guards of a change depend on what it does, not only on the keys it touches: turning billing on locks
// workspaces, rotating its token does not. configGuards reads the change from the version that runs to the
// version it would save and says, for each key that needs one, what it would do (preview), what to type
// (phrase) and whether it is tried first (D12). The console shows these; PATCH checks them again, so an API
// client, an approved token's request and the console pass the same gates. A phrase never comes from a
// secret's value.

// TrialDuration is how long a trial change runs before it reverts by itself (D12). End-to-end builds
// shorten it.
var TrialDuration = 10 * time.Minute

type configGuard struct {
	Key     string `json:"key"`
	Preview string `json:"preview,omitempty"`
	Phrase  string `json:"phrase,omitempty"`
	Trial   bool   `json:"trial,omitempty"`
	// Refused is set when the change cannot be saved as it is (a product missing, a slug nobody has).
	Refused string `json:"refused,omitempty"`
}

func enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func valueOf(snap serverconfig.Snapshot, name string) string {
	value, _ := snap.Get(name)
	return strings.TrimSpace(value)
}

// configGuards lists the guards of the change from current to next, for the keys it touches.
func (a *API) configGuards(ctx context.Context, request *http.Request, current, next serverconfig.Snapshot, touched []string) []configGuard {
	var guards []configGuard
	seen := map[string]bool{}
	add := func(guard configGuard) {
		if seen[guard.Key] {
			return
		}
		seen[guard.Key] = true
		guards = append(guards, guard)
	}
	for _, name := range touched {
		before, after := valueOf(current, name), valueOf(next, name)
		if before == after && name != "polar.access_token" {
			continue
		}
		switch {
		case name == "polar.access_token" || name == "polar.webhook_secret" || name == "polar.api_url":
			on, wasOn := valueOf(next, "polar.access_token") != "", valueOf(current, "polar.access_token") != ""
			if on == wasOn {
				continue
			}
			if on {
				locked, err := a.preview.LockedWithBilling(ctx, a.now())
				if err != nil {
					add(configGuard{Key: "polar.access_token", Refused: "The workspaces billing would lock could not be counted; try again."})
					continue
				}
				add(configGuard{Key: "polar.access_token", Phrase: "billing on", Preview: fmt.Sprintf("Billing turns this server into the hosted cloud: "+
					"%s whose trial ended without a plan lock at once, and every new workspace starts a trial.", workspacesText(locked))})
			} else {
				add(configGuard{Key: "polar.access_token", Phrase: "billing off", Preview: "Billing turns off: every workspace gets full access " +
					"with no trial and no lock, and retention deletes nothing."})
			}
		case name == "retention.delete":
			if !enabled(after) || enabled(before) {
				continue
			}
			if valueOf(next, "polar.access_token") == "" {
				add(configGuard{Key: name, Preview: "Billing is off, so no workspace is locked and nothing is deleted while it stays off."})
				continue
			}
			due, err := a.retentionDue(ctx)
			if err != nil {
				add(configGuard{Key: name, Refused: "The workspaces due for deletion could not be counted; try again."})
				continue
			}
			perDay := valueOf(next, "retention.max_deletions_per_day")
			if perDay == "" {
				perDay = "5"
			}
			add(configGuard{Key: name, Phrase: strconv.Itoa(due), Preview: fmt.Sprintf("Deletion starts: %s locked for 12 months %s due now, "+
				"deleted at most %s a day after a last check with Polar. A deleted workspace cannot be brought back.", workspacesText(due), isAre(due), perDay)})
		case name == "email.from_policy":
			if after != "any" {
				continue
			}
			add(configGuard{Key: name, Phrase: "any", Preview: "Every workspace may send as any address through your relay. " +
				"On a relay you share with others, one customer can then pose as another."})
		case name == "mail_from.domain" || name == "hosted_mail.domain":
			if before == "" {
				continue
			}
			phrase := after
			if phrase == "" {
				phrase = "remove " + before
			}
			add(configGuard{Key: name, Phrase: phrase, Preview: fmt.Sprintf("Replies to mail sent before go to addresses at %s: once it changes, "+
				"they no longer thread into their conversations.", before)})
		case name == "ai.allow_private_base_urls":
			if !enabled(after) || enabled(before) {
				continue
			}
			add(configGuard{Key: name, Phrase: "allow private addresses", Preview: "A workspace admin can then point the AI provider at an " +
				"address inside your network, and the server will call it (server-side request forgery)."})
		case name == "trusted_proxies":
			preview := "No proxy is trusted: every visitor's address is the connection's own."
			if proxies, err := ParseTrustedProxies(after); err == nil && len(proxies) > 0 {
				preview = "Under this list your own address reads as " + clientIP(request, proxies) + "."
			}
			add(configGuard{Key: name, Trial: true, Preview: preview + " It is tried for " + minutes(TrialDuration) +
				"; confirm it from a page that still works, or it reverts by itself."})
		case strings.HasPrefix(name, "cloud_auth.") || name == "clerk.publishable_key":
			issuer := valueOf(next, "cloud_auth.issuer")
			phrase := issuer
			if phrase == "" {
				phrase = "cloud sign-in off"
			}
			add(configGuard{Key: "cloud_auth.issuer", Phrase: phrase, Trial: true, Preview: "Who can sign in to the hosted cloud changes. Operators " +
				"do not sign in through it, so the console counts the cloud sign-ins that work and fail meanwhile. It is tried for " +
				minutes(TrialDuration) + " and reverts by itself unless you confirm it."})
		case name == "help_center.domain":
			preview := "Help centers move back to " + strings.TrimRight(a.config.Origin, "/") + "/help/<slug>."
			if after != "" {
				if refused := a.wildcardMissing(ctx, after); refused != "" {
					add(configGuard{Key: name, Refused: refused})
					continue
				}
				preview = "Every public help center moves to <slug>." + after + "; old links stop working unless redirected, and your reverse proxy needs certificates for them."
			}
			add(configGuard{Key: name, Trial: true, Preview: preview + " It is tried for " + minutes(TrialDuration) + " and reverts by itself unless you confirm it."})
		case name == "problem_report.workspace":
			if after == "" {
				continue
			}
			exists, err := a.preview.WorkspaceExists(ctx, after)
			switch {
			case err != nil:
				add(configGuard{Key: name, Refused: "The workspace could not be looked up; try again."})
			case !exists:
				add(configGuard{Key: name, Refused: "No workspace has the address " + after + "."})
			default:
				add(configGuard{Key: name, Preview: "Problem reports from every workspace become conversations in " + after + "."})
			}
		case name == "kb_video.max_bytes":
			add(configGuard{Key: name, Preview: "Videos up to this size upload in parts of 8 MiB: your reverse proxy must accept bodies that large."})
		}
	}
	return guards
}

// retentionDue counts the workspaces the retention pass would delete now.
func (a *API) retentionDue(ctx context.Context) (int, error) {
	items, err := retention.New(retention.Config{DB: a.db}).Plan(ctx)
	if err != nil {
		return 0, err
	}
	due := 0
	for _, item := range items {
		if item.Action == retention.ActionDelete {
			due++
		}
	}
	return due, nil
}

func workspacesText(n int) string {
	if n == 1 {
		return "1 workspace"
	}
	return strconv.Itoa(n) + " workspaces"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func minutes(d time.Duration) string {
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + " seconds"
	}
	return strconv.Itoa(int(d.Minutes())) + " minutes"
}

// guardsTrial reports whether any guard tries the change first.
func guardsTrial(guards []configGuard) bool {
	return slices.ContainsFunc(guards, func(guard configGuard) bool { return guard.Trial })
}

// confirmOperatorConfigTrial keeps a trial change (D12).
func (a *API) confirmOperatorConfigTrial(response http.ResponseWriter, request *http.Request) {
	a.decideTrial(response, request, true)
}

// revertOperatorConfigTrial ends a trial change now.
func (a *API) revertOperatorConfigTrial(response http.ResponseWriter, request *http.Request) {
	a.decideTrial(response, request, false)
}

func (a *API) decideTrial(response http.ResponseWriter, request *http.Request, keep bool) {
	var input struct {
		Version    int64  `json:"version"`
		ReauthCode string `json:"reauth_code"`
		Note       string `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Version < 1 {
		writeRefused(response, http.StatusBadRequest, "invalid_json", "Send the version of the trial.", nil)
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	actor := serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}
	operation := "trial-revert"
	if keep {
		operation = "trial-confirm"
	}
	a.operatorOnce(response, request, operation, []string{strconv.FormatInt(input.Version, 10)}, func(ctx context.Context) (any, any, error) {
		if keep {
			if err := a.config.ServerConfig.ConfirmTrial(ctx, input.Version, actor); err != nil {
				return nil, nil, err
			}
			saved := map[string]any{"version": input.Version, "applies": "at_once"}
			return saved, saved, nil
		}
		version, err := a.config.ServerConfig.RevertTrial(ctx, input.Version, actor)
		if err != nil {
			return nil, nil, err
		}
		saved := map[string]any{"version": version, "applies": "in_seconds"}
		return saved, saved, nil
	})
}

// trialEvidence is what this process saw since a trial started, to judge it before confirming: the
// distinct client addresses and the cloud sign-ins that worked and failed. It counts in memory, per process.
type trialEvidence struct {
	mu        sync.Mutex
	version   int64
	addresses map[string]struct{}
	ok        int
	refused   int
}

// observe records one request's client address while a trial runs; it costs a map write.
func (e *trialEvidence) observe(version int64, address string) {
	if e == nil || version == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.version != version {
		e.version, e.addresses, e.ok, e.refused = version, map[string]struct{}{}, 0, 0
	}
	if len(e.addresses) < 10_000 {
		e.addresses[address] = struct{}{}
	}
}

// signIn records the outcome of one cloud sign-in while a trial runs.
func (e *trialEvidence) signIn(version int64, ok bool) {
	if e == nil || version == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.version != version {
		e.version, e.addresses, e.ok, e.refused = version, map[string]struct{}{}, 0, 0
	}
	if ok {
		e.ok++
	} else {
		e.refused++
	}
}

// countTrialSignIn records a verified or refused cloud sign-in while a trial change runs.
func (a *API) countTrialSignIn(ok bool) {
	if a.config.Carry != nil && a.config.Carry.Hot != nil {
		a.config.Carry.trial.signIn(a.config.Carry.Hot.Trial(), ok)
	}
}

func (e *trialEvidence) read(version int64) (addresses, ok, refused int) {
	if e == nil {
		return 0, 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.version != version {
		return 0, 0, 0
	}
	return len(e.addresses), e.ok, e.refused
}

// listOperatorProducts lists the Polar products.
func (a *API) listOperatorProducts(response http.ResponseWriter, request *http.Request) {
	products, err := a.config.ServerConfig.Products(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The products could not be read.")
		return
	}
	if products == nil {
		products = []serverconfig.ProductRow{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": products})
}

var productPlans = []string{"team", "founding", "solo", "small", "team25"}
var productPeriods = []string{"monthly", "yearly", "single"}

// addOperatorProduct adds a Polar product; products are never removed nor changed (add-only).
func (a *API) addOperatorProduct(response http.ResponseWriter, request *http.Request) {
	var input struct {
		ProductID  string `json:"product_id"`
		Plan       string `json:"plan"`
		Period     string `json:"period"`
		OnSale     *bool  `json:"on_sale"`
		ReauthCode string `json:"reauth_code"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeRefused(response, http.StatusBadRequest, "invalid_json", "The body is not a product.", nil)
		return
	}
	row := serverconfig.ProductRow{ProductID: strings.TrimSpace(input.ProductID), Plan: input.Plan, Period: input.Period, OnSale: input.OnSale == nil || *input.OnSale}
	fields := map[string]string{}
	if row.ProductID == "" || len(row.ProductID) > 200 {
		fields["product_id"] = "Enter the product's id in Polar."
	}
	if !slices.Contains(productPlans, row.Plan) {
		fields["plan"] = "Choose a plan."
	}
	if !slices.Contains(productPeriods, row.Period) {
		fields["period"] = "Choose a period."
	}
	if len(fields) > 0 {
		writeRefused(response, http.StatusBadRequest, "invalid_product", "Nothing was added.", fields)
		return
	}
	existing, err := a.config.ServerConfig.Products(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The products could not be read.")
		return
	}
	if slices.ContainsFunc(existing, func(product serverconfig.ProductRow) bool { return product.ProductID == row.ProductID }) {
		writeError(response, http.StatusConflict, "product_exists", "That product is listed already; a product is never changed, only taken off sale.")
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	actor := serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}
	a.operatorOnce(response, request, "product-add", []string{row.ProductID, row.Plan, row.Period, strconv.FormatBool(row.OnSale)}, func(ctx context.Context) (any, any, error) {
		if err := a.config.ServerConfig.AddProduct(ctx, row, actor); err != nil {
			return nil, nil, err
		}
		return row, row, nil
	})
}

// setOperatorProductOnSale puts a product on or off sale; nothing else of it changes.
func (a *API) setOperatorProductOnSale(response http.ResponseWriter, request *http.Request) {
	var input struct {
		OnSale     *bool  `json:"on_sale"`
		ReauthCode string `json:"reauth_code"`
	}
	if err := decodeJSON(request, &input); err != nil || input.OnSale == nil {
		writeRefused(response, http.StatusBadRequest, "invalid_json", "Send on_sale.", nil)
		return
	}
	id := request.PathValue("id")
	products, err := a.config.ServerConfig.Products(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The products could not be read.")
		return
	}
	index := slices.IndexFunc(products, func(product serverconfig.ProductRow) bool { return product.ProductID == id })
	if index < 0 {
		writeError(response, http.StatusNotFound, "not_found", "No such product.")
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	if err := a.config.ServerConfig.SetOnSale(request.Context(), id, *input.OnSale, serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}); err != nil {
		slog.Error("set a product on sale", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The product could not be changed.")
		return
	}
	row := products[index]
	row.OnSale = *input.OnSale
	writeJSON(response, http.StatusOK, row)
}

// tryPolar asks Polar about each product the change sells with its token: the token works and no product
// is archived (an archived one stops every checkout). Nothing is saved; an error never repeats a secret.
func (a *API) tryPolar(ctx context.Context, snap serverconfig.Snapshot) map[string]any {
	config, err := polar.ConfigFromEnv(snap.Getenv)
	if err != nil || !config.Enabled() {
		return map[string]any{"name": "polar", "ok": false, "message": "Set the Polar token, its webhook secret and the Team and Founding products first."}
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := polar.NewClient(config)
	for _, id := range []string{config.Products.TeamMonthly, config.Products.TeamYearly, config.Products.Founding} {
		archived, err := client.ProductArchived(callCtx, id)
		if err != nil {
			return map[string]any{"name": "polar", "ok": false, "message": "Polar refused: " + redactSecretsIn(err.Error(), snap)}
		}
		if archived {
			return map[string]any{"name": "polar", "ok": false, "message": "Product " + id + " is archived at Polar: nobody can buy it."}
		}
	}
	return map[string]any{"name": "polar", "ok": true, "message": "Polar answered with this token; the Team and Founding products are on sale. Nothing was saved."}
}

// tryJWKS fetches the cloud sign-in keys of the change once. Nothing is saved.
func tryJWKS(ctx context.Context, snap serverconfig.Snapshot) map[string]any {
	config := cloudauth.ConfigFromEnv(snap.Getenv)
	if !config.Enabled() {
		return map[string]any{"name": "jwks", "ok": false, "message": "Set the issuer and the JWKS address first."}
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	count, err := cloudauth.New(config).CheckKeys(callCtx)
	if err != nil {
		return map[string]any{"name": "jwks", "ok": false, "message": "The JWKS address did not answer with keys: " + err.Error()}
	}
	if count == 0 {
		return map[string]any{"name": "jwks", "ok": false, "message": "The JWKS address answered with no RSA key: no cloud sign-in would verify."}
	}
	return map[string]any{"name": "jwks", "ok": true, "message": fmt.Sprintf("The JWKS address answered with %d keys. Nothing was saved.", count)}
}

// wildcardMissing says why the help center domain cannot be used yet: every workspace's help center is
// <slug>.<domain>, so a name nobody created must resolve (a wildcard record). It looks up one made-up name;
// "" when it resolves.
func (a *API) wildcardMissing(ctx context.Context, domain string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := "dns-check-" + strconv.FormatInt(a.now().UnixNano()%1_000_000, 36) + "." + domain
	if a.config.LookupCNAME == nil {
		return ""
	}
	if _, err := a.config.LookupCNAME(lookupCtx, probe); err != nil {
		return "No DNS record answers for " + probe + ": add a wildcard record *." + domain + " that points at this server first."
	}
	return ""
}

// helpCenterEvidence fetches a real help center under the domain a trial sets, as a visitor would.
func (a *API) helpCenterEvidence(ctx context.Context, domain string) map[string]any {
	slug, err := a.preview.AnyOpenHelpCenter(ctx)
	if err != nil || slug == "" {
		return map[string]any{"ok": false, "message": "No workspace has its help center on, so no page could be fetched."}
	}
	address := "https://" + helpcenter.SlugHost(domain, slug) + "/"
	probe := a.config.HelpCenterProbe
	if probe == nil {
		probe = fetchStatus
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := probe(fetchCtx, address)
	if err != nil {
		return map[string]any{"url": address, "ok": false, "message": "The page did not answer: " + err.Error()}
	}
	return map[string]any{"url": address, "status": status, "ok": status == http.StatusOK}
}

func fetchStatus(ctx context.Context, address string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	_ = response.Body.Close()
	return response.StatusCode, nil
}

// tryAI sends one tiny request to the server AI provider of the change. Nothing is saved; an error never
// repeats the key.
func (a *API) tryAI(ctx context.Context, snap serverconfig.Snapshot) map[string]any {
	server, err := ai.ServerProviderFromEnv(snap.Getenv)
	if err != nil || server == nil {
		return map[string]any{"name": "ai", "ok": false, "message": "Set the provider, its key or address and the models first."}
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	model, err := a.ai.PingServer(callCtx, server)
	if err != nil {
		return map[string]any{"name": "ai", "ok": false, "message": "The provider refused " + model + ": " + redactSecretsIn(err.Error(), snap)}
	}
	return map[string]any{"name": "ai", "ok": true, "message": "The provider answered with " + model + ". Nothing was saved."}
}
