package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/jackc/pgx/v5"
)

// The operator console's configuration routes (docs/internals/operator-console.md). The server enforces
// every guard the console shows (the code, the secret entered again, the phrase typed): an API client or
// an approved token's request passes the same checks.

// consoleEditable says whether the console changes key to value yet. The settings that need a preview with
// numbers, a typed phrase or a 10-minute trial (billing, retention, mail domains in use, trusted proxies,
// cloud sign-in, the help center domain, private AI addresses, any sender) are changed with the CLI until
// their guards ship; a version that would change them is refused here.
func consoleEditable(key serverconfig.Key, value string) (bool, string) {
	if key.Name == "operator.require_totp" && strings.TrimSpace(value) != "true" {
		return false, "an authenticator can be turned off only with collie config, and never on the hosted cloud"
	}
	return true, ""
}

type operatorConfigChange struct {
	BaseVersion int64              `json:"base_version"`
	Set         map[string]string  `json:"set"`
	Unset       []string           `json:"unset"`
	Secrets     map[string]*string `json:"secrets"`
	Confirm     map[string]string  `json:"confirm"`
	ReauthCode  string             `json:"reauth_code"`
	Note        string             `json:"note"`
}

func writeRefused(response http.ResponseWriter, status int, code, message string, fields map[string]string) {
	body := map[string]any{"code": code, "message": message}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	writeJSON(response, status, map[string]any{"error": body})
}

// operatorCurrent loads the configuration every process should run.
func (a *API) operatorCurrent(ctx context.Context) (serverconfig.Loaded, error) {
	return a.config.ServerConfig.Current(ctx)
}

// friendlyConfigError turns a build error into one message per setting: the parsers name variables, the
// console names settings.
func friendlyConfigError(err error) (string, map[string]string) {
	text := err.Error()
	fields := map[string]string{}
	// A product has no setting of its own: name it as the products table does.
	for _, product := range serverconfig.ProductEnvs {
		text = strings.ReplaceAll(text, product.Env, "the "+product.Plan+" "+product.Period+" product (Billing › Products)")
	}
	text = strings.ReplaceAll(text, "(or unset every POLAR_* variable to turn billing off)", "(or remove the Polar settings to turn billing off)")
	for _, key := range serverconfig.Keys() {
		if key.Env != "" && strings.Contains(text, key.Env) {
			text = strings.ReplaceAll(text, key.Env, key.Name)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		for _, key := range serverconfig.Keys() {
			if strings.Contains(line, key.Name) {
				if _, seen := fields[key.Name]; !seen {
					fields[key.Name] = line
				}
			}
		}
	}
	return text, fields
}

// confirmMatches compares a typed phrase as the console does: trimmed, case-insensitive.
func confirmMatches(typed, phrase string) bool {
	return strings.EqualFold(strings.TrimSpace(typed), strings.TrimSpace(phrase))
}

func (a *API) changeOperatorConfig(response http.ResponseWriter, request *http.Request) {
	var input operatorConfigChange
	if err := decodeJSON(request, &input); err != nil {
		writeRefused(response, http.StatusBadRequest, "invalid_json", "The body is not a configuration change.", nil)
		return
	}
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		slog.Error("read the configuration", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The configuration could not be read.")
		return
	}
	set, unset := map[string]string{}, []string{}
	needsReauth := false
	var touched []serverconfig.Key
	for name, value := range input.Set {
		key, ok := serverconfig.Lookup(name)
		if !ok {
			writeRefused(response, http.StatusBadRequest, "unknown_setting", "Unknown setting. Nothing was saved.", map[string]string{name: "Unknown setting."})
			return
		}
		if key.Secret {
			writeRefused(response, http.StatusBadRequest, "secret_in_set", "Secrets go in secrets, never in set. Nothing was saved.", map[string]string{key.Name: "A secret goes in secrets."})
			return
		}
		set[key.Name] = strings.TrimSpace(value)
		touched = append(touched, key)
	}
	for _, name := range input.Unset {
		key, ok := serverconfig.Lookup(name)
		if !ok {
			writeRefused(response, http.StatusBadRequest, "unknown_setting", "Unknown setting. Nothing was saved.", map[string]string{name: "Unknown setting."})
			return
		}
		unset = append(unset, key.Name)
		touched = append(touched, key)
	}
	for name, value := range input.Secrets {
		key, ok := serverconfig.Lookup(name)
		if !ok || !key.Secret {
			writeRefused(response, http.StatusBadRequest, "unknown_setting", "Not a secret setting. Nothing was saved.", map[string]string{name: "Not a secret."})
			return
		}
		if value == nil || strings.TrimSpace(*value) == "" {
			writeRefused(response, http.StatusBadRequest, "secret_missing", "Enter the secret. Nothing was saved.", map[string]string{key.Name: "Enter it."})
			return
		}
		if !current.Ring() {
			writeRefused(response, http.StatusBadRequest, "secret_keys_missing", "Secrets can be saved only with a key ring (MAILBOX_TOKEN_KEYS). Nothing was saved.", map[string]string{key.Name: "No key ring."})
			return
		}
		set[key.Name] = *value
		needsReauth = true
		touched = append(touched, key)
	}
	if len(touched) == 0 {
		writeRefused(response, http.StatusBadRequest, "nothing_to_save", "The change is empty.", nil)
		return
	}
	reenter := map[string]string{}
	for _, key := range touched {
		value := set[key.Name]
		if ok, why := consoleEditable(key, value); !ok {
			writeRefused(response, http.StatusBadRequest, "setting_not_changeable", "This setting is not changed in the console yet. Nothing was saved.", map[string]string{key.Name: why})
			return
		}
		if slices.Contains(key.Guards, serverconfig.GuardReauth) {
			needsReauth = true
		}
		// The secret an endpoint is bound to is typed again when the endpoint moves and the secret stays: a
		// removal sends it nowhere.
		before, _ := current.Get(key.Name)
		if slices.Contains(key.Guards, serverconfig.GuardReenterSecret) && key.Reenter != "" && before != value && !slices.Contains(unset, key.Reenter) {
			if _, isSet := current.Get(key.Reenter); isSet {
				if _, given := input.Secrets[key.Reenter]; !given {
					reenter[key.Reenter] = "Enter it again: it goes to the new " + key.Name + "."
				}
			}
		}
	}
	if len(reenter) > 0 {
		writeRefused(response, http.StatusBadRequest, "reenter_secret", "Enter the secret again for the new address. Nothing was saved.", reenter)
		return
	}
	next := current.With(set, unset)
	if a.config.CheckConfig != nil {
		if err := a.config.CheckConfig(next); err != nil {
			message, fields := friendlyConfigError(err)
			writeRefused(response, http.StatusUnprocessableEntity, "config_invalid", message, fields)
			return
		}
	}
	if refused := switchesRefused(current.Snapshot, next); refused != nil {
		writeRefused(response, http.StatusUnprocessableEntity, "config_invalid", "A feature cannot be turned on before its settings are complete. Nothing was saved.", refused.Fields)
		return
	}
	names := make([]string, 0, len(touched))
	for _, key := range touched {
		names = append(names, key.Name)
	}
	guards := a.configGuards(request.Context(), request, current.Snapshot, next, names)
	for _, guard := range guards {
		if guard.Refused != "" {
			writeRefused(response, http.StatusUnprocessableEntity, "config_invalid", guard.Refused+" Nothing was saved.", map[string]string{guard.Key: guard.Refused})
			return
		}
	}
	for _, guard := range guards {
		if guard.Phrase != "" && !confirmMatches(input.Confirm[guard.Key], guard.Phrase) {
			writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "confirm_mismatch",
				"message": "Type the phrase to confirm. Nothing was saved.", "fields": map[string]string{guard.Key: "Type " + guard.Phrase + " to confirm."}, "guards": guards}})
			return
		}
		// A phrase or a trial is a decision that needs the code; a preview alone only informs.
		if guard.Phrase != "" || guard.Trial {
			needsReauth = true
		}
	}
	if needsReauth && !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	change := serverconfig.Change{Base: input.BaseVersion, CheckBase: true, Set: set, Unset: unset, Source: "console", Note: strings.TrimSpace(input.Note),
		Actor: serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}}
	if requestID, ok := operatorRequestIDOf(request.Context()); ok {
		change.Source, change.RequestID = "request", requestID
	}
	if guardsTrial(guards) {
		change.Trial = TrialDuration
	}
	version, err := a.config.ServerConfig.Save(request.Context(), change)
	switch {
	case errors.Is(err, serverconfig.ErrTrialPending):
		writeRefused(response, http.StatusConflict, "trial_pending", "A trial change runs: confirm or revert it before saving another. Nothing was saved.", nil)
		return
	case errors.Is(err, serverconfig.ErrConflict):
		writeJSON(response, http.StatusConflict, map[string]any{"error": map[string]any{"code": "config_changed",
			"message": "Someone saved another version since you read this one. Nothing was saved.", "version": current.Version}})
		return
	case errors.Is(err, serverconfig.ErrNoChange):
		writeRefused(response, http.StatusBadRequest, "nothing_to_save", "Every setting already has that value.", nil)
		return
	case err != nil:
		slog.Error("save the configuration", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The configuration could not be saved.")
		return
	}
	changed := make([]string, 0, len(touched))
	applies := "at_once"
	for _, key := range touched {
		changed = append(changed, key.Name)
		if !key.Hot {
			applies = "in_seconds"
		}
	}
	slices.Sort(changed)
	out := map[string]any{"version": version, "applies": applies, "changed": slices.Compact(changed)}
	if change.Trial > 0 {
		out["applies"], out["trial_expires_at"] = "trial", a.now().Add(change.Trial).UTC()
	}
	writeJSON(response, http.StatusOK, out)
}

type operatorRequestIDKey struct{}

func operatorRequestIDOf(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(operatorRequestIDKey{}).(string)
	return id, ok
}

// maskURL shows a database URL's host and database, never its user or password.
func maskURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "(set)"
	}
	return parsed.Scheme + "://…@" + parsed.Host + parsed.Path
}

func (a *API) getOperatorConfig(response http.ResponseWriter, request *http.Request) {
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		slog.Error("read the configuration", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The configuration could not be read.")
		return
	}
	environ := a.environ()
	settings := make([]map[string]any, 0, len(serverconfig.Keys()))
	for _, key := range serverconfig.Keys() {
		value, set := current.Get(key.Name)
		row := map[string]any{"key": key.Name, "group": key.Group, "secret": key.Secret, "hot": key.Hot, "default": key.Default,
			"set": set, "source": "default", "guards": guardNames(key.Guards), "editable": true, "locked": ""}
		if key.Env != "" {
			row["env_name"] = key.Env
		}
		if key.Reenter != "" {
			row["reenter"] = key.Reenter
		}
		if key.Secret {
			row["value"] = nil
		} else if set {
			row["value"] = value
		} else {
			row["value"] = nil
		}
		if meta, ok := current.Meta[key.Name]; ok {
			row["source"], row["set_by"], row["set_at"], row["version"] = meta.Source, meta.By, meta.At.UTC(), meta.Version
			if meta.KeyID != "" {
				row["key_id"] = meta.KeyID
			}
		} else if set {
			row["source"] = "environment"
		}
		switch {
		case key.Secret && !current.Ring():
			row["locked"] = "no_key_ring"
		case key.Secret && slices.Contains(current.Unopened, key.Name):
			row["locked"] = "stale_key"
		case key.Name == "telemetry.enabled" && (environ("DISABLE_TELEMETRY") != "" || environ("DO_NOT_TRACK") != ""):
			row["locked"] = "env_off"
		}
		settings = append(settings, row)
	}
	bootstrap := []map[string]any{}
	for _, name := range serverconfig.Bootstrap {
		value := environ(name)
		row := map[string]any{"env_name": name, "set": value != ""}
		switch {
		case value == "":
			row["value"] = nil
		case strings.HasSuffix(name, "_URL"):
			row["value"] = maskURL(value)
		case name == "MAILBOX_TOKEN_KEYS":
			row["value"] = strings.Join(a.config.ServerConfig.Keys().IDs(), ", ")
		default:
			row["value"] = value
		}
		bootstrap = append(bootstrap, row)
	}
	who := operatorPrincipal(request)
	writeJSON(response, http.StatusOK, map[string]any{"version": current.Version, "settings": settings, "bootstrap": bootstrap,
		"key_ring":          map[string]any{"configured": current.Ring(), "key_ids": a.config.ServerConfig.Keys().IDs(), "current": a.config.ServerConfig.Keys().Current()},
		"telemetry_env_off": environ("DISABLE_TELEMETRY") != "" || environ("DO_NOT_TRACK") != "", "operator_email": who.Operator.Email,
		"features": serverconfig.FeatureStates(current.Snapshot)})
}

func guardNames(guards []serverconfig.Guard) []string {
	names := make([]string, len(guards))
	for i, guard := range guards {
		names[i] = string(guard)
	}
	return names
}

// environ reads the process environment for the console's bootstrap and leftover views; nil reads nothing.
func (a *API) environ() func(string) string {
	if a.config.Environ != nil {
		return a.config.Environ
	}
	return func(string) string { return "" }
}

func (a *API) getOperatorStatus(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	current, err := a.operatorCurrent(ctx)
	if err != nil {
		slog.Error("read the configuration", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The status could not be read.")
		return
	}
	processes, err := a.config.ServerConfig.Processes(ctx)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The status could not be read.")
		return
	}
	migration, _ := a.config.ServerConfig.LatestMigration(ctx)
	waiting, _ := a.config.Operators.PendingRequests(ctx)
	warnings := []map[string]any{}
	for _, leftover := range serverconfig.Leftovers(current.Snapshot, a.environ()) {
		code, severity := "env_differs", "error"
		if leftover.Same {
			code, severity = "env_same", "warning"
		}
		warnings = append(warnings, map[string]any{"code": code, "severity": severity, "env_name": leftover.Env, "key": leftover.Key})
	}
	for _, name := range serverconfig.Dead(a.environ()) {
		warnings = append(warnings, map[string]any{"code": "env_dead", "severity": "warning", "env_name": name})
	}
	if !current.Ring() {
		warnings = append(warnings, map[string]any{"code": "no_key_ring", "severity": "warning", "env_name": "MAILBOX_TOKEN_KEYS"})
	}
	for _, key := range current.Unopened {
		warnings = append(warnings, map[string]any{"code": "secret_stale_key", "severity": "error", "key": key})
	}
	if history, err := a.config.ServerConfig.History(ctx, 1); err == nil && len(history) == 1 && history[0].Status == "rejected" {
		warnings = append(warnings, map[string]any{"code": "version_refused", "severity": "error", "version": history[0].ID, "detail": history[0].Rejected})
	}
	process := make([]map[string]any, 0, len(processes))
	for _, item := range processes {
		process = append(process, map[string]any{"instance_id": item.InstanceID, "hostname": item.Hostname, "binary_version": item.BinaryVersion,
			"running_version": item.RunningVersion, "started_at": item.StartedAt.UTC(), "heartbeat_at": item.HeartbeatAt.UTC(),
			"last_error": item.LastError, "waiting_for": item.WaitingFor})
		if item.WaitingFor != "" {
			warnings = append(warnings, map[string]any{"code": "waiting_for_import", "severity": "warning", "detail": item.WaitingFor})
		}
	}
	if a.config.OperatorOrigin == "" {
		// On the hosted cloud a console on the app's origin is an error: a bug in the app would reach it.
		severity := "warning"
		if a.config.Billing.Enabled() {
			severity = "error"
		}
		warnings = append(warnings, map[string]any{"code": "path_mode", "severity": severity, "env_name": "OPERATOR_ORIGIN"})
	}
	if waiting > 0 {
		warnings = append(warnings, map[string]any{"code": "requests_waiting", "severity": "warning", "detail": strconv.Itoa(waiting)})
	}
	// A feature switched on whose settings went missing reads as off; the console says what it misses.
	for _, state := range serverconfig.FeatureStates(current.Snapshot) {
		if state.Requested == "on" && !state.Enabled {
			warnings = append(warnings, map[string]any{"code": "feature_incomplete", "severity": "warning", "key": state.SwitchKey, "detail": strings.Join(state.Missing, ", ")})
		}
	}
	var trial any
	if run, ok, err := a.config.ServerConfig.Trial(ctx); err == nil && ok {
		var addresses, signedIn, refused int
		if a.config.Carry != nil {
			addresses, signedIn, refused = a.config.Carry.trial.read(run.Version)
		}
		evidence := map[string]any{"your_ip": clientIP(request, a.config.TrustedProxies),
			"client_ips": addresses, "cloud_sign_ins_ok": signedIn, "cloud_sign_ins_refused": refused}
		if domain, _ := current.Get("help_center.domain"); slices.Contains(run.Keys, "help_center.domain") && strings.TrimSpace(domain) != "" {
			evidence["help_center"] = a.helpCenterEvidence(ctx, strings.TrimSpace(domain))
		}
		trial = map[string]any{"version": run.Version, "parent_version": run.Parent, "started_at": run.StartedAt.UTC(), "expires_at": run.ExpiresAt.UTC(),
			"started_by": run.StartedBy, "keys": run.Keys, "evidence": evidence}
	}
	writeJSON(response, http.StatusOK, map[string]any{"binary_version": a.config.Version, "migration": migration, "desired_version": current.State.Desired,
		"last_good_version": current.State.LastGood, "processes": process, "warnings": warnings, "requests_waiting": waiting, "trial": trial,
		"console": map[string]any{"origin": a.config.Operators.ConsoleBase(), "path_mode": a.config.OperatorOrigin == ""}})
}

func (a *API) checkOperatorConfig(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Set     map[string]string  `json:"set"`
		Unset   []string           `json:"unset"`
		Secrets map[string]*string `json:"secrets"`
		Test    string             `json:"test"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_json", "The body is not a configuration change.")
		return
	}
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The configuration could not be read.")
		return
	}
	set := map[string]string{}
	for name, value := range input.Set {
		if key, ok := serverconfig.Lookup(name); ok && !key.Secret {
			set[key.Name] = strings.TrimSpace(value)
		}
	}
	for name, value := range input.Secrets {
		if key, ok := serverconfig.Lookup(name); ok && key.Secret && value != nil {
			set[key.Name] = *value
		}
	}
	next := current.With(set, input.Unset)
	out := map[string]any{"ok": true, "errors": map[string]string{}}
	if a.config.CheckConfig != nil {
		if err := a.config.CheckConfig(next); err != nil {
			text, fields := friendlyConfigError(err)
			if len(fields) == 0 {
				fields = map[string]string{"": text}
			}
			out["ok"], out["errors"] = false, fields
		}
	}
	if refused := switchesRefused(current.Snapshot, next); refused != nil && out["ok"] == true {
		out["ok"], out["errors"] = false, refused.Fields
	}
	if out["ok"] == true {
		var names []string
		for name := range set {
			names = append(names, name)
		}
		for _, name := range input.Unset {
			if key, ok := serverconfig.Lookup(name); ok {
				names = append(names, key.Name)
			}
		}
		slices.Sort(names)
		guards := a.configGuards(request.Context(), request, current.Snapshot, next, names)
		for _, guard := range guards {
			if guard.Refused != "" {
				out["ok"] = false
				out["errors"].(map[string]string)[guard.Key] = guard.Refused
			}
		}
		if guards == nil {
			guards = []configGuard{}
		}
		out["guards"] = guards
	}
	if input.Test != "" && out["ok"] == true {
		// A live test sends the stored secrets: never at a token's word (it only reads), and never to an
		// address the change moves without the secret typed again, as a save would ask.
		if operatorPrincipal(request).IsToken() {
			writeError(response, http.StatusForbidden, "test_needs_a_person", "A live test sends the server's secrets; only a person runs it in the console. Without test, the change is checked.")
			return
		}
		for name := range set {
			key, _ := serverconfig.Lookup(name)
			if !slices.Contains(key.Guards, serverconfig.GuardReenterSecret) || key.Reenter == "" {
				continue
			}
			before, _ := current.Get(key.Name)
			if _, stored := current.Get(key.Reenter); stored && before != set[key.Name] {
				if value, typed := input.Secrets[key.Reenter]; !typed || value == nil || *value == "" {
					out["ok"] = false
					out["test"] = map[string]any{"name": input.Test, "ok": false, "message": "Enter " + key.Reenter + " again: the test goes to the new " + key.Name + "."}
					writeJSON(response, http.StatusOK, out)
					return
				}
			}
		}
	}
	if out["ok"] == true {
		switch input.Test {
		case "smtp":
			out["test"] = a.sendTestEmail(request.Context(), next, operatorPrincipal(request).Operator.Email)
		case "polar":
			out["test"] = a.tryPolar(request.Context(), next)
		case "jwks":
			out["test"] = tryJWKS(request.Context(), next)
		case "ai":
			out["test"] = a.tryAI(request.Context(), next)
		}
	}
	writeJSON(response, http.StatusOK, out)
}

// sendTestEmail sends one short email to the operator through the relay of the change, saving nothing.
func (a *API) sendTestEmail(ctx context.Context, snap serverconfig.Snapshot, to string) map[string]any {
	config, err := email.ConfigFromEnv(snap.Getenv)
	if err != nil || !config.SMTP.Configured() {
		return map[string]any{"name": "smtp", "ok": false, "message": "Set the relay host first."}
	}
	from := to
	if config.MailFromDomain != "" {
		from = "noreply@" + config.MailFromDomain
	}
	messageID, err := email.NewMessageID(strings.SplitN(from, "@", 2)[1])
	if err != nil {
		return map[string]any{"name": "smtp", "ok": false, "message": "The test email could not be written."}
	}
	message, err := email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: to, Subject: "Colliedesk test email",
		Body: "This is a test from the operator console. Nothing was saved.", MessageID: messageID, Date: a.now(), Standalone: true})
	if err != nil {
		return map[string]any{"name": "smtp", "ok": false, "message": "The test email could not be written."}
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := (email.SMTPSender{Config: config.SMTP}).Send(sendCtx, from, []string{to}, message); err != nil {
		return map[string]any{"name": "smtp", "ok": false, "message": "The relay refused the test email: " + redactSecretsIn(err.Error(), snap)}
	}
	return map[string]any{"name": "smtp", "ok": true, "message": "Sent a test email to " + to + ". Nothing was saved."}
}

// redactSecretsIn hides any secret value a provider's error repeats.
func redactSecretsIn(text string, snap serverconfig.Snapshot) string {
	for _, key := range serverconfig.Keys() {
		if value, ok := snap.Get(key.Name); key.Secret && ok && len(value) >= 4 {
			text = strings.ReplaceAll(text, value, "[secret]")
		}
	}
	return text
}

func versionJSON(version serverconfig.Version) map[string]any {
	changes := version.Changes
	if changes == nil {
		changes = []serverconfig.ChangeItem{}
	}
	return map[string]any{"id": version.ID, "created_at": version.CreatedAt.UTC(), "created_by": version.CreatedBy, "source": version.Source,
		"parent_id": version.ParentID, "note": version.Note, "status": version.Status, "rejected_reason": version.Rejected, "changes": changes}
}

func (a *API) listOperatorConfigVersions(response http.ResponseWriter, request *http.Request) {
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(response, http.StatusBadRequest, "invalid_limit", "limit is 1 to 200.")
			return
		}
		limit = parsed
	}
	versions, err := a.config.ServerConfig.History(request.Context(), limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The history could not be read.")
		return
	}
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The history could not be read.")
		return
	}
	items := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		items = append(items, versionJSON(version))
	}
	writeJSON(response, http.StatusOK, map[string]any{"running_version": current.Version, "items": items})
}

func (a *API) restoreOperatorConfigVersion(response http.ResponseWriter, request *http.Request) {
	from, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || from < 1 {
		writeError(response, http.StatusNotFound, "not_found", "No such version.")
		return
	}
	var input struct {
		BaseVersion int64             `json:"base_version"`
		ReauthCode  string            `json:"reauth_code"`
		Note        string            `json:"note"`
		Confirm     map[string]string `json:"confirm"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_json", "Send the version you read (base_version).")
		return
	}
	old, err := a.config.ServerConfig.Load(request.Context(), from)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The version could not be read.")
		return
	}
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The configuration could not be read.")
		return
	}
	if history, _ := a.config.ServerConfig.History(request.Context(), 200); !slices.ContainsFunc(history, func(v serverconfig.Version) bool { return v.ID == from }) && from > current.Version {
		writeError(response, http.StatusNotFound, "not_found", "No such version.")
		return
	}
	var names []string
	for name := range diffKeys(current.Snapshot, old.Snapshot) {
		key, _ := serverconfig.Lookup(name)
		value, _ := old.Get(name)
		if ok, why := consoleEditable(key, value); !ok {
			writeRefused(response, http.StatusBadRequest, "setting_not_changeable", "This version changes a setting the console does not change yet; restore it with collie config restore. Nothing was saved.", map[string]string{key.Name: why})
			return
		}
		names = append(names, name)
	}
	slices.Sort(names)
	// A restore is a change like any other: it passes the guards of what it changes from the version that runs
	// (turning billing off asks "billing off"; a restore of trusted proxies is a trial).
	guards := a.configGuards(request.Context(), request, current.Snapshot, old.Snapshot, names)
	for _, guard := range guards {
		if guard.Refused != "" {
			writeRefused(response, http.StatusUnprocessableEntity, "config_invalid", guard.Refused+" Nothing was saved.", map[string]string{guard.Key: guard.Refused})
			return
		}
		if guard.Phrase != "" && !confirmMatches(input.Confirm[guard.Key], guard.Phrase) {
			writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "confirm_mismatch",
				"message": "Type the phrase to confirm. Nothing was saved.", "fields": map[string]string{guard.Key: "Type " + guard.Phrase + " to confirm."}, "guards": guards}})
			return
		}
	}
	var trial time.Duration
	if guardsTrial(guards) {
		trial = TrialDuration
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	if a.config.CheckConfig != nil {
		if err := a.config.CheckConfig(old.Snapshot); err != nil {
			message, fields := friendlyConfigError(err)
			writeRefused(response, http.StatusUnprocessableEntity, "config_invalid", message, fields)
			return
		}
	}
	who := operatorPrincipal(request)
	a.operatorOnce(response, request, "config-restore", []string{strconv.FormatInt(from, 10), strconv.FormatInt(input.BaseVersion, 10)}, func(ctx context.Context) (any, any, error) {
		version, err := a.config.ServerConfig.RestoreTrial(ctx, from, input.BaseVersion, serverconfig.Actor{Kind: "operator", Name: who.Operator.Email, IP: clientIP(request, a.config.TrustedProxies)}, input.Note, trial)
		if err != nil {
			return nil, nil, err
		}
		saved := map[string]any{"version": version, "applies": "in_seconds"}
		if trial > 0 {
			saved["applies"], saved["trial_expires_at"] = "trial", a.now().Add(trial).UTC()
		}
		return saved, saved, nil
	})
}

func diffKeys(before, after serverconfig.Snapshot) map[string]bool {
	keys := map[string]bool{}
	old, next := before.Values(), after.Values()
	for key, value := range next {
		if previous, ok := old[key]; !ok || previous != value {
			keys[key] = true
		}
	}
	for key := range old {
		if _, ok := next[key]; !ok {
			keys[key] = true
		}
	}
	return keys
}

func (a *API) listOperatorAudit(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	var since time.Time
	if raw := query.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_since", "since is an RFC 3339 time.")
			return
		}
		since = parsed
	}
	limit, _ := strconv.Atoi(query.Get("limit"))
	entries, err := a.config.ServerConfig.AuditLog(request.Context(), query.Get("key"), query.Get("actor"), since, limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The audit log could not be read.")
		return
	}
	if entries == nil {
		entries = []serverconfig.AuditEntry{}
	}
	for i := range entries {
		entries[i].At = entries[i].At.UTC()
		if entries[i].Keys == nil {
			entries[i].Keys = []string{}
		}
		if entries[i].Detail == nil {
			entries[i].Detail = map[string]any{}
		}
	}
	if query.Get("format") == "csv" {
		response.Header().Set("Content-Type", "text/csv; charset=utf-8")
		response.Header().Set("Content-Disposition", `attachment; filename="operator-audit.csv"`)
		writer := csv.NewWriter(response)
		_ = writer.Write([]string{"at", "actor_kind", "actor", "action", "version", "settings", "ip"})
		for _, entry := range entries {
			version := ""
			if entry.VersionID != nil {
				version = strconv.FormatInt(*entry.VersionID, 10)
			}
			_ = writer.Write([]string{entry.At.Format(time.RFC3339), entry.ActorKind, entry.Actor, entry.Action, version, strings.Join(entry.Keys, " "), entry.IP})
		}
		writer.Flush()
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": entries})
}

func operatorRequestJSON(request auth.OperatorRequest) map[string]any {
	var body any
	if len(request.Body) > 0 {
		body = request.Body
	}
	var result any
	if len(request.Result) > 0 {
		result = request.Result
	}
	return map[string]any{"id": request.ID, "session_id": request.SessionID, "token_label": request.TokenLabel, "email": request.Email,
		"method": request.Method, "route": request.Route, "path": request.Path, "body": body, "summary": request.Summary, "note": request.Note,
		"needs_secrets": request.NeedsSecrets, "status": request.Status, "decided_by": request.DecidedBy, "decided_at": request.DecidedAt,
		"result_status": request.ResultStatus, "result": result, "created_at": request.CreatedAt.UTC(), "expires_at": request.ExpiresAt.UTC()}
}

func (a *API) listOperatorRequests(response http.ResponseWriter, request *http.Request) {
	requests, err := a.config.Operators.Requests(request.Context(), operatorPrincipal(request))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The requests could not be read.")
		return
	}
	items := make([]map[string]any, 0, len(requests))
	for _, item := range requests {
		items = append(items, a.operatorRequestShown(request, item))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) getOperatorRequest(response http.ResponseWriter, request *http.Request) {
	found, err := a.config.Operators.Request(request.Context(), operatorPrincipal(request), request.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(response, http.StatusNotFound, "not_found", "No such request.")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be read.")
		return
	}
	writeJSON(response, http.StatusOK, a.operatorRequestShown(request, found))
}

// operatorRequestShown is a request as a person reviews it: a configuration change waiting for them also
// says what it does and what to type (its guards), as the review of a change in the console does.
func (a *API) operatorRequestShown(request *http.Request, found auth.OperatorRequest) map[string]any {
	shown := operatorRequestJSON(found)
	if found.Status != "pending" || found.Route != "PATCH /api/v1/operator/config" || operatorPrincipal(request).IsToken() {
		return shown
	}
	// The secrets it names are typed at approval; a placeholder stands for them so a guard that depends on
	// one being set (billing on) shows.
	placeholders := map[string]string{}
	for _, name := range found.NeedsSecrets {
		placeholders[name] = "(typed at approval)"
	}
	guards, _ := a.requestGuards(request, found.Body, placeholders, nil)
	if guards == nil {
		guards = []configGuard{}
	}
	shown["guards"] = guards
	return shown
}

func (a *API) cancelOperatorRequest(response http.ResponseWriter, request *http.Request) {
	found, err := a.config.Operators.CancelRequest(request.Context(), operatorPrincipal(request), request.PathValue("id"))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(response, http.StatusNotFound, "not_found", "No such request.")
	case errors.Is(err, auth.ErrOperatorRequestDecided):
		writeError(response, http.StatusConflict, "request_decided", "The request was decided, cancelled or expired.")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be cancelled.")
	default:
		writeJSON(response, http.StatusOK, operatorRequestJSON(found))
	}
}

func (a *API) listOperators(response http.ResponseWriter, request *http.Request) {
	operators, err := a.config.Operators.Operators(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The operators could not be read.")
		return
	}
	sessions, err := a.config.Operators.Sessions(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The sessions could not be read.")
		return
	}
	rows := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		rows = append(rows, operatorSessionJSON(session))
	}
	if operators == nil {
		operators = []auth.Operator{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"operators": operators, "sessions": rows})
}

func (a *API) inviteOperator(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email      string `json:"email"`
		ReauthCode string `json:"reauth_code"`
	}
	if err := decodeJSON(request, &input); err != nil || !strings.Contains(input.Email, "@") {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the operator's email address.")
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	email := strings.ToLower(strings.TrimSpace(input.Email))
	a.operatorOnce(response, request, "operator-invite", []string{email}, func(ctx context.Context) (any, any, error) {
		link, expires, err := a.config.Operators.Invite(ctx, who, email)
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"email": email, "link": link, "expires_at": expires.UTC()}, map[string]any{"email": email, "expires_at": expires.UTC()}, nil
	})
}

func (a *API) revokeOperator(response http.ResponseWriter, request *http.Request) {
	if !a.reauthorized(response, request, request.Header.Get("X-Colliedesk-Reauth")) {
		return
	}
	who := operatorPrincipal(request)
	found, err := a.config.Operators.RevokeOperator(request.Context(), request.PathValue("id"), actorKindOf(who), who.Operator.Email)
	switch {
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "The operator could not be revoked.")
	case !found:
		writeError(response, http.StatusNotFound, "not_found", "No such operator.")
	default:
		response.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) revokeOperatorSession(response http.ResponseWriter, request *http.Request) {
	who := operatorPrincipal(request)
	id := request.PathValue("id")
	if id != who.SessionID && !a.reauthorized(response, request, request.Header.Get("X-Colliedesk-Reauth")) {
		return
	}
	found, err := a.config.Operators.RevokeSession(request.Context(), id, actorKindOf(who), who.Operator.Email)
	switch {
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "The session could not be ended.")
	case !found:
		writeError(response, http.StatusNotFound, "not_found", "No such session.")
	default:
		if id == who.SessionID && who.ViaCookie {
			a.setOperatorCookie(response, "", -1)
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func actorKindOf(who auth.OperatorPrincipal) string {
	if who.IsToken() {
		return "token"
	}
	return "operator"
}

// operatorWithheld are the routes whose answer is a credential: an approved request shows it to the
// approver once and keeps only its status.
var operatorWithheld = map[string]bool{"POST /api/v1/operator/tokens": true, "POST /api/v1/operator/operators": true}

// decideOperatorRequest is the operator branch of the two approval routes (X-Colliedesk-Realm: operator):
// an operator's browser session decides an operator request; nothing of a workspace is looked up.
func (a *API) decideOperatorRequest(approve bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		raw, viaCookie, ok := operatorCredential(request, a.operatorCookie())
		if !ok {
			writeError(response, http.StatusUnauthorized, "unauthorized", "An operator session is required.")
			return
		}
		who, err := a.config.Operators.Authenticate(request.Context(), raw, viaCookie)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "unauthorized", "An operator session is required.")
			return
		}
		if who.IsToken() {
			writeError(response, http.StatusForbidden, "person_only", "Only a person decides an operator request; an approval by a token would mean nothing.")
			return
		}
		if writes(request.Method) && request.Header.Get("Origin") != a.operatorOrigin() {
			writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match the operator console.")
			return
		}
		ctx := context.WithValue(request.Context(), operatorPrincipalKey{}, who)
		request = request.WithContext(ctx)
		var input struct {
			ReauthCode string            `json:"reauth_code"`
			Secrets    map[string]string `json:"secrets"`
			Confirm    map[string]string `json:"confirm"`
			Reason     string            `json:"reason"`
		}
		if request.ContentLength != 0 {
			if err := decodeJSON(request, &input); err != nil {
				writeError(response, http.StatusBadRequest, "invalid_json", "The body is not valid.")
				return
			}
		}
		id := request.PathValue("id")
		pending, err := a.config.Operators.Request(ctx, who, id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(response, http.StatusNotFound, "not_found", "No such request.")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be read.")
			return
		}
		if pending.Status != "pending" {
			writeError(response, http.StatusConflict, "request_decided", "The request was decided, cancelled or expired.")
			return
		}
		// The phrases are checked before the code is spent: a wrong phrase costs no authenticator code.
		if approve && pending.Route == "PATCH /api/v1/operator/config" {
			if guards, mismatch := a.requestGuards(request, pending.Body, input.Secrets, input.Confirm); mismatch != "" {
				writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "confirm_mismatch",
					"message": "Type the phrase to confirm. The request keeps waiting.", "fields": map[string]string{mismatch: "Type the phrase to confirm."}, "guards": guards}})
				return
			}
		}
		if !a.reauthorized(response, request, input.ReauthCode) {
			return
		}
		if !approve {
			rejected, err := a.config.Operators.RejectRequest(ctx, who, id)
			if err != nil {
				writeError(response, http.StatusConflict, "request_decided", "The request was decided, cancelled or expired.")
				return
			}
			writeJSON(response, http.StatusOK, operatorRequestJSON(rejected))
			return
		}
		for name := range input.Secrets {
			if !slices.Contains(pending.NeedsSecrets, name) {
				writeError(response, http.StatusBadRequest, "secret_not_asked", "The request does not ask for that secret.")
				return
			}
		}
		claimed, err := a.config.Operators.ClaimRequest(ctx, who, id)
		if err != nil {
			writeError(response, http.StatusConflict, "request_decided", "The request was decided, cancelled or expired.")
			return
		}
		status, body := a.replayOperatorRequest(ctx, who, claimed, input.Secrets, input.Confirm)
		kept := json.RawMessage(body)
		if operatorWithheld[claimed.Route] || !json.Valid(body) {
			kept = nil
		}
		finished, err := a.config.Operators.FinishRequest(context.WithoutCancel(ctx), id, status, kept)
		if err != nil {
			slog.Error("record an operator request's outcome", "error", err)
			finished = claimed
		}
		var result any
		if json.Valid(body) && len(body) > 0 {
			result = json.RawMessage(body)
		}
		writeJSON(response, http.StatusOK, map[string]any{"request": operatorRequestJSON(finished), "result": result})
	}
}

// replayOperatorRequest runs the stored call once, as the approver who just gave their code. The secrets
// the approver typed reach the call in memory only: never the stored body, the result or the log.
func (a *API) replayOperatorRequest(ctx context.Context, approver auth.OperatorPrincipal, stored auth.OperatorRequest, secrets, confirm map[string]string) (int, []byte) {
	body := map[string]any{}
	if len(stored.Body) > 0 {
		_ = json.Unmarshal(stored.Body, &body)
	}
	if len(secrets) > 0 {
		filled := map[string]any{}
		for name, value := range secrets {
			filled[name] = value
		}
		body["secrets"] = filled
	}
	delete(body, "reauth_code")
	delete(body, "confirm")
	// The approver reviewed the change against the version that runs now (the dialog shows that diff), so the
	// change applies on it: a version saved since the token asked does not fail the request.
	if _, based := body["base_version"]; based {
		if current, err := a.operatorCurrent(ctx); err == nil {
			body["base_version"] = current.Version
		}
	}
	if len(confirm) > 0 {
		body["confirm"] = confirm
	}
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequestWithContext(context.WithoutCancel(ctx), stored.Method, stored.Path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	replayCtx := context.WithValue(request.Context(), operatorApprovalKey{}, approver.MarkReauthenticated(a.config.Operators.Now()))
	replayCtx = context.WithValue(replayCtx, operatorRequestIDKey{}, stored.ID)
	recorder := httptest.NewRecorder()
	a.mux.ServeHTTP(recorder, request.WithContext(replayCtx))
	out, _ := io.ReadAll(recorder.Result().Body)
	return recorder.Code, out
}

// requestGuards works out what a filed configuration change asks the approver to type, with the secrets
// they entered, and names the first phrase that does not match (empty when every one does).
func (a *API) requestGuards(request *http.Request, body json.RawMessage, secrets, confirm map[string]string) ([]configGuard, string) {
	var change operatorConfigChange
	if len(body) > 0 && json.Unmarshal(body, &change) != nil {
		return nil, ""
	}
	current, err := a.operatorCurrent(request.Context())
	if err != nil {
		return nil, ""
	}
	set := map[string]string{}
	var names []string
	for name, value := range change.Set {
		if key, ok := serverconfig.Lookup(name); ok && !key.Secret {
			set[key.Name] = strings.TrimSpace(value)
			names = append(names, key.Name)
		}
	}
	for name, value := range secrets {
		if key, ok := serverconfig.Lookup(name); ok && key.Secret {
			set[key.Name] = value
			names = append(names, key.Name)
		}
	}
	for _, name := range change.Unset {
		if key, ok := serverconfig.Lookup(name); ok {
			names = append(names, key.Name)
		}
	}
	slices.Sort(names)
	guards := a.configGuards(request.Context(), request, current.Snapshot, current.With(set, change.Unset), names)
	for _, guard := range guards {
		if guard.Phrase != "" && !confirmMatches(confirm[guard.Key], guard.Phrase) {
			return guards, guard.Key
		}
	}
	return guards, ""
}

// switchesRefused is serverconfig.CheckSwitches as a refusal per switch, nil when none.
func switchesRefused(current, next serverconfig.Snapshot) *serverconfig.SwitchError {
	var refused *serverconfig.SwitchError
	if errors.As(serverconfig.CheckSwitches(current, next), &refused) {
		return refused
	}
	return nil
}
