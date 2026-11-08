package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/configerr"
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
	// The codes of Preview and Refused, for the console (docs/internals/operator-console.md › Message codes).
	PreviewCode   string           `json:"preview_code,omitempty"`
	PreviewParams configerr.Params `json:"preview_params,omitempty"`
	RefusedCode   string           `json:"refused_code,omitempty"`
	RefusedParams configerr.Params `json:"refused_params,omitempty"`
}

// previews is the guard with the preview said.
func (g configGuard) previews(code string, params configerr.Params) configGuard {
	said := configerr.Say(code, params)
	g.Preview, g.PreviewCode, g.PreviewParams = said.Text, said.Code, said.Params
	return g
}

// refuses is the guard refused for what is said.
func (g configGuard) refuses(code string, params configerr.Params) configGuard {
	said := configerr.Say(code, params)
	g.Refused, g.RefusedCode, g.RefusedParams = said.Text, said.Code, said.Params
	return g
}

// refusedSaid is the guard's refusal with its code.
func (g configGuard) refusedSaid() *configerr.Error {
	return &configerr.Error{Code: g.RefusedCode, Params: g.RefusedParams, Text: g.Refused}
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
		case strings.HasPrefix(name, "features."):
			if guard, ok := a.switchGuard(ctx, strings.TrimPrefix(name, "features."), current, next); ok {
				add(guard)
			}
		case name == "polar.access_token" || name == "polar.webhook_secret" || name == "polar.api_url":
			on, wasOn := valueOf(next, "polar.access_token") != "", valueOf(current, "polar.access_token") != ""
			if on == wasOn {
				continue
			}
			if on {
				locked, err := a.preview.LockedWithBilling(ctx, a.now())
				if err != nil {
					add(configGuard{Key: "polar.access_token"}.refuses("guard_billing_uncountable", nil))
					continue
				}
				add(configGuard{Key: "polar.access_token", Phrase: "billing on"}.previews("guard_billing_on", configerr.Params{"workspaces": locked}))
			} else {
				add(configGuard{Key: "polar.access_token", Phrase: "billing off"}.previews("guard_billing_off", nil))
			}
		case name == "retention.delete":
			if !enabled(after) || enabled(before) {
				continue
			}
			if valueOf(next, "polar.access_token") == "" {
				add(configGuard{Key: name}.previews("guard_retention_billing_off", nil))
				continue
			}
			due, err := a.retentionDue(ctx)
			if err != nil {
				add(configGuard{Key: name}.refuses("guard_retention_uncountable", nil))
				continue
			}
			perDay := valueOf(next, "retention.max_deletions_per_day")
			if perDay == "" {
				perDay = "5"
			}
			add(configGuard{Key: name, Phrase: strconv.Itoa(due)}.previews("guard_retention_on", configerr.Params{"workspaces": due, "per_day": perDay}))
		case name == "email.from_policy":
			if after != "any" {
				continue
			}
			add(configGuard{Key: name, Phrase: "any"}.previews("guard_from_policy_any", nil))
		case name == "mail_from.domain" || name == "hosted_mail.domain":
			if before == "" {
				continue
			}
			phrase := after
			if phrase == "" {
				phrase = "remove " + before
			}
			add(configGuard{Key: name, Phrase: phrase}.previews("guard_mail_domain_moves", configerr.Params{"before": before}))
		case name == "ai.allow_private_base_urls":
			if !enabled(after) || enabled(before) {
				continue
			}
			add(configGuard{Key: name, Phrase: "allow private addresses"}.previews("guard_private_addresses", nil))
		case name == "trusted_proxies":
			guard := configGuard{Key: name, Trial: true}.previews("guard_proxies_none", trialParams(nil))
			if proxies, err := ParseTrustedProxies(after); err == nil && len(proxies) > 0 {
				guard = guard.previews("guard_proxies_read", trialParams(configerr.Params{"address": clientIP(request, proxies)}))
			}
			add(guard)
		case strings.HasPrefix(name, "cloud_auth.") || name == "clerk.publishable_key":
			issuer := valueOf(next, "cloud_auth.issuer")
			phrase := issuer
			if phrase == "" {
				phrase = "cloud sign-in off"
			}
			add(configGuard{Key: "cloud_auth.issuer", Phrase: phrase, Trial: true}.previews("guard_cloud_auth", trialParams(nil)))
		case name == "help_center.domain":
			guard := configGuard{Key: name, Trial: true}.previews("guard_help_center_back", trialParams(configerr.Params{"origin": strings.TrimRight(a.config.Origin, "/")}))
			if after != "" {
				if probe := a.wildcardMissing(ctx, after); probe != "" {
					add(configGuard{Key: name}.refuses("guard_help_center_no_wildcard", configerr.Params{"probe": probe, "domain": after}))
					continue
				}
				guard = guard.previews("guard_help_center_moves", trialParams(configerr.Params{"domain": after}))
			}
			add(guard)
		case name == "problem_report.workspace":
			if after == "" {
				continue
			}
			exists, err := a.preview.WorkspaceExists(ctx, after)
			switch {
			case err != nil:
				add(configGuard{Key: name}.refuses("guard_workspace_unlooked", nil))
			case !exists:
				add(configGuard{Key: name}.refuses("guard_no_workspace", configerr.Params{"slug": after}))
			default:
				add(configGuard{Key: name}.previews("guard_problem_reports", configerr.Params{"slug": after}))
			}
		case name == "kb_video.max_bytes":
			add(configGuard{Key: name}.previews("guard_kb_video", nil))
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

// trialParams adds how long a trial runs to params: trial in minutes, or in seconds (seconds = 1) when end-to-end
// builds shorten it below a minute.
func trialParams(params configerr.Params) configerr.Params {
	if params == nil {
		params = configerr.Params{}
	}
	params["trial"], params["seconds"] = int(TrialDuration.Minutes()), 0
	if TrialDuration < time.Minute {
		params["trial"], params["seconds"] = int(TrialDuration.Seconds()), 1
	}
	return params
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
		writeRefusedSaid(response, http.StatusBadRequest, "invalid_json", configerr.Say("send_trial_version", nil), nil)
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
		writeSaid(response, http.StatusInternalServerError, "internal_error", configerr.Say("products_unreadable", nil))
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
		writeRefusedSaid(response, http.StatusBadRequest, "invalid_json", configerr.Say("body_not_a_product", nil), nil)
		return
	}
	row := serverconfig.ProductRow{ProductID: strings.TrimSpace(input.ProductID), Plan: input.Plan, Period: input.Period, OnSale: input.OnSale == nil || *input.OnSale}
	fields := map[string]*configerr.Error{}
	if row.ProductID == "" || len(row.ProductID) > 200 {
		fields["product_id"] = configerr.Say("field_product_id", nil)
	}
	if !slices.Contains(productPlans, row.Plan) {
		fields["plan"] = configerr.Say("field_choose_plan", nil)
	}
	if !slices.Contains(productPeriods, row.Period) {
		fields["period"] = configerr.Say("field_choose_period", nil)
	}
	if len(fields) > 0 {
		writeRefusedSaid(response, http.StatusBadRequest, "invalid_product", configerr.Say("nothing_added", nil), fields)
		return
	}
	existing, err := a.config.ServerConfig.Products(request.Context())
	if err != nil {
		writeSaid(response, http.StatusInternalServerError, "internal_error", configerr.Say("products_unreadable", nil))
		return
	}
	if slices.ContainsFunc(existing, func(product serverconfig.ProductRow) bool { return product.ProductID == row.ProductID }) {
		writeSaid(response, http.StatusConflict, "product_exists", configerr.Say("product_exists", nil))
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
		writeRefusedSaid(response, http.StatusBadRequest, "invalid_json", configerr.Say("send_on_sale", nil), nil)
		return
	}
	id := request.PathValue("id")
	products, err := a.config.ServerConfig.Products(request.Context())
	if err != nil {
		writeSaid(response, http.StatusInternalServerError, "internal_error", configerr.Say("products_unreadable", nil))
		return
	}
	index := slices.IndexFunc(products, func(product serverconfig.ProductRow) bool { return product.ProductID == id })
	if index < 0 {
		writeSaid(response, http.StatusNotFound, "not_found", configerr.Say("no_such_product", nil))
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	if err := a.config.ServerConfig.SetOnSale(request.Context(), id, *input.OnSale, serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}); err != nil {
		slog.Error("set a product on sale", "error", err)
		writeSaid(response, http.StatusInternalServerError, "internal_error", configerr.Say("product_unchanged", nil))
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
		return testSaid("polar", false, configerr.Say("test_polar_needs_settings", nil))
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := polar.NewClient(config)
	for _, id := range []string{config.Products.TeamMonthly, config.Products.TeamYearly, config.Products.Founding} {
		archived, err := client.ProductArchived(callCtx, id)
		if err != nil {
			return testSaid("polar", false, configerr.Say("test_polar_refused", configerr.Params{"detail": redactSecretsIn(err.Error(), snap)}))
		}
		if archived {
			return testSaid("polar", false, configerr.Say("test_polar_archived", configerr.Params{"product": id}))
		}
	}
	return testSaid("polar", true, configerr.Say("test_polar_ok", nil))
}

// tryJWKS fetches the cloud sign-in keys of the change once. Nothing is saved.
func tryJWKS(ctx context.Context, snap serverconfig.Snapshot) map[string]any {
	config := cloudauth.ConfigFromEnv(snap.Getenv)
	if !config.Enabled() {
		return testSaid("jwks", false, configerr.Say("test_jwks_needs_settings", nil))
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	count, err := cloudauth.New(config).CheckKeys(callCtx)
	if err != nil {
		return testSaid("jwks", false, configerr.Say("test_jwks_failed", configerr.Params{"detail": err.Error()}))
	}
	if count == 0 {
		return testSaid("jwks", false, configerr.Say("test_jwks_no_rsa", nil))
	}
	return testSaid("jwks", true, configerr.Say("test_jwks_ok", configerr.Params{"count": count}))
}

// wildcardMissing says why the help center domain cannot be used yet: every workspace's help center is
// <slug>.<domain>, so a name nobody created must resolve (a wildcard record). It looks up one made-up name and
// returns it when it does not resolve; "" when it resolves.
func (a *API) wildcardMissing(ctx context.Context, domain string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := "dns-check-" + strconv.FormatInt(a.now().UnixNano()%1_000_000, 36) + "." + domain
	if a.config.LookupCNAME == nil {
		return ""
	}
	if _, err := a.config.LookupCNAME(lookupCtx, probe); err != nil {
		return probe
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
		return testSaid("ai", false, configerr.Say("test_ai_needs_settings", nil))
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	model, err := a.ai.PingServer(callCtx, server)
	if err != nil {
		return testSaid("ai", false, configerr.Say("test_ai_refused", configerr.Params{"model": model, "detail": redactSecretsIn(err.Error(), snap)}))
	}
	return testSaid("ai", true, configerr.Say("test_ai_ok", configerr.Params{"model": model}))
}

// listServerAIModels asks the server AI provider of the change which models its key may use. Nothing is saved; an
// error never repeats the key.
func (a *API) listServerAIModels(ctx context.Context, snap serverconfig.Snapshot) map[string]any {
	value := func(key string) string { return strings.TrimSpace(snap.Getenv(key)) }
	kind, base, key := aiprovider.Kind(value("AI_SERVER_PROVIDER")), value("AI_SERVER_BASE_URL"), value("AI_SERVER_API_KEY")
	refuseCoded := func(code string, params configerr.Params) map[string]any {
		return testSaid("ai_models", false, configerr.Say(code, params))
	}
	switch {
	case kind != aiprovider.KindAnthropic && kind != aiprovider.KindOpenAICompatible:
		return refuseCoded("test_ai_models_needs_provider", nil)
	case kind == aiprovider.KindOpenAICompatible && base == "":
		return refuseCoded("test_ai_models_needs_base_url", nil)
	case kind == aiprovider.KindAnthropic && key == "":
		return refuseCoded("test_ai_models_needs_key", nil)
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	models, prices, err := a.ai.ListServerModels(callCtx, kind, base, key)
	if err != nil {
		var check *ai.CheckError
		if errors.As(err, &check) {
			return refuseCoded("test_ai_models_refused", configerr.Params{"check_code": string(check.Code)})
		}
		return refuseCoded("test_ai_models_failed", configerr.Params{"detail": redactSecretsIn(err.Error(), snap)})
	}
	out := testSaid("ai_models", true, configerr.Say("test_ai_models_ok", configerr.Params{"count": len(models)}))
	out["models"], out["prices"] = models, prices
	return out
}

// switchGuard is what switching a feature on or off asks: who it stops, counted, and for billing and cloud
// sign-in a phrase (and a trial for cloud sign-in). Switching a feature off removes nothing.
func (a *API) switchGuard(ctx context.Context, feature string, current, next serverconfig.Snapshot) (configGuard, bool) {
	key := serverconfig.SwitchKey(feature)
	was, now := featureEnabled(current, feature), featureEnabled(next, feature)
	if was == now {
		return configGuard{}, false
	}
	if now {
		if feature != serverconfig.FeatureBilling {
			return configGuard{}, false
		}
		locked, err := a.preview.LockedWithBilling(ctx, a.now())
		if err != nil {
			return configGuard{Key: key}.refuses("guard_billing_uncountable", nil), true
		}
		return configGuard{Key: key, Phrase: "billing on"}.previews("guard_billing_on_again", configerr.Params{"workspaces": locked}), true
	}
	switch feature {
	case serverconfig.FeatureGmail, serverconfig.FeatureMicrosoft:
		provider, label := "google", "Gmail"
		if feature == serverconfig.FeatureMicrosoft {
			provider, label = "microsoft", "Microsoft 365"
		}
		channels, workspaces, err := a.preview.MailboxConnections(ctx, provider)
		if err != nil {
			return configGuard{Key: key}.refuses("guard_mailbox_uncountable", configerr.Params{"label": label}), true
		}
		guard := configGuard{Key: key}.previews("guard_mailbox_off", configerr.Params{"label": label, "channels": channels, "workspaces": workspaces})
		if channels > 0 {
			guard.Phrase = feature + " off"
		}
		return guard, true
	case serverconfig.FeatureAIServer:
		workspaces, err := a.preview.ServerAIWorkspaces(ctx)
		if err != nil {
			return configGuard{Key: key}.refuses("guard_ai_server_uncountable", nil), true
		}
		return configGuard{Key: key}.previews("guard_ai_server_off", configerr.Params{"workspaces": workspaces}), true
	case serverconfig.FeatureAutofix:
		return configGuard{Key: key}.previews("guard_autofix_off", nil), true
	case serverconfig.FeatureHostedMail:
		return configGuard{Key: key}.previews("guard_hosted_mail_off", nil), true
	case serverconfig.FeatureSignup:
		return configGuard{Key: key}.previews("guard_signup_off", nil), true
	case serverconfig.FeatureCloudSignIn:
		return configGuard{Key: key, Phrase: "cloud sign-in off", Trial: true}.previews("guard_cloud_sign_in_off", trialParams(nil)), true
	case serverconfig.FeatureBilling:
		return configGuard{Key: key, Phrase: "billing off"}.previews("guard_billing_switch_off", nil), true
	}
	return configGuard{}, false
}

func featureEnabled(snap serverconfig.Snapshot, feature string) bool {
	for _, state := range serverconfig.FeatureStates(snap) {
		if state.Name == feature {
			return state.Enabled
		}
	}
	return false
}
