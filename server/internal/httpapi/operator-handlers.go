package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/qrsvg"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// The operator console's routes (docs/internals/operator-console.md). They take only an operator's
// session (cookie) or operator token (cdop_ bearer): a workspace session or agent token is refused (401),
// and an operator credential never opens a workspace route (the cookie has its own name, the token its
// own table).

const operatorCookieName = "colliedesk_operator"

type operatorPrincipalKey struct{}

func operatorPrincipal(request *http.Request) auth.OperatorPrincipal {
	return request.Context().Value(operatorPrincipalKey{}).(auth.OperatorPrincipal)
}

// operatorOrigin is the origin the console is served from: OPERATOR_ORIGIN, else APP_ORIGIN.
func (a *API) operatorOrigin() string {
	if a.config.OperatorOrigin != "" {
		return a.config.OperatorOrigin
	}
	return a.config.Origin
}

// operatorCookie is the cookie's name: __Host- over https, so no other host can set or read it.
func (a *API) operatorCookie() string {
	if strings.HasPrefix(a.operatorOrigin(), "https://") {
		return "__Host-" + operatorCookieName
	}
	return operatorCookieName
}

func (a *API) setOperatorCookie(response http.ResponseWriter, raw string, maxAge int) {
	http.SetCookie(response, &http.Cookie{Name: a.operatorCookie(), Value: raw, Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func operatorCredential(request *http.Request, cookieName string) (string, bool, bool) {
	if header := request.Header.Get("Authorization"); header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || len(header) == len(prefix) {
			return "", false, false
		}
		return strings.TrimSpace(strings.TrimPrefix(header, prefix)), false, true
	}
	cookie, err := request.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return "", false, false
	}
	return cookie.Value, true, true
}

// authorizeOperator is authorize for the operator routes. An operator token's call to a route that needs a
// person (Approval) or a permission it lacks becomes an operator request (202).
func (a *API) authorizeOperator(route Route, next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		var who auth.OperatorPrincipal
		if replay, ok := operatorApprovalOf(request.Context()); ok {
			who = replay
		} else {
			raw, viaCookie, ok := operatorCredential(request, a.operatorCookie())
			if !ok {
				writeError(response, http.StatusUnauthorized, "unauthorized", "An operator session is required.")
				return
			}
			var err error
			who, err = a.config.Operators.Authenticate(request.Context(), raw, viaCookie)
			if err != nil {
				if !errors.Is(err, auth.ErrInvalidCredential) {
					slog.Error("authenticate operator", "error", err)
				}
				writeError(response, http.StatusUnauthorized, "unauthorized", "An operator session is required.")
				return
			}
			if viaCookie && writes(request.Method) && request.Header.Get("Origin") != a.operatorOrigin() {
				writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match the operator console.")
				return
			}
			if who.IsToken() {
				access := tokenTiers[route.Method+" "+route.Pattern]
				if access.Approval || (access.Permission != "" && !who.Allows(access.Permission)) {
					a.fileOperatorRequest(response, request, route, who)
					return
				}
			}
		}
		ctx := context.WithValue(request.Context(), operatorPrincipalKey{}, who)
		next(response, request.WithContext(ctx))
	}
}

type operatorApprovalKey struct{}

// operatorApprovalOf is the approving operator when an approved operator request is replayed in process.
func operatorApprovalOf(ctx context.Context) (auth.OperatorPrincipal, bool) {
	who, ok := ctx.Value(operatorApprovalKey{}).(auth.OperatorPrincipal)
	return who, ok
}

// secretInBody reports whether a JSON body carries the value of a secret: a token may only ask for one
// ("secrets": {"smtp.password": null}), never send it; the approver types it.
func secretInBody(body []byte) bool {
	var shape struct {
		Secrets map[string]any `json:"secrets"`
	}
	if json.Unmarshal(body, &shape) != nil {
		return false
	}
	for _, value := range shape.Secrets {
		if value != nil {
			return true
		}
	}
	return false
}

// fileOperatorRequest stores an operator token's call as a request a person approves. The body is kept
// as sent, without any secret (a body with one is refused and nothing is stored).
func (a *API) fileOperatorRequest(response http.ResponseWriter, request *http.Request, route Route, who auth.OperatorPrincipal) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 256<<10))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_body", "The body could not be read.")
		return
	}
	if secretInBody(body) {
		writeError(response, http.StatusBadRequest, "secret_in_request", "A token cannot send a secret. Ask for it with null (\"secrets\": {\"smtp.password\": null}); the person who approves types it.")
		return
	}
	var shape struct {
		Note    string         `json:"note"`
		Secrets map[string]any `json:"secrets"`
		Set     map[string]any `json:"set"`
		Unset   []string       `json:"unset"`
	}
	var stored json.RawMessage
	if len(bytes.TrimSpace(body)) > 0 {
		if !json.Valid(body) {
			writeError(response, http.StatusBadRequest, "invalid_json", "The body is not valid JSON.")
			return
		}
		_ = json.Unmarshal(body, &shape)
		for name := range shape.Set {
			if key, ok := serverconfig.Lookup(name); ok && key.Secret {
				writeError(response, http.StatusBadRequest, "secret_in_request", "A token cannot send a secret. Ask for it with null in secrets; the person who approves types it.")
				return
			}
		}
		stored = withoutApproverFields(body)
	}
	var needs []string
	for name := range shape.Secrets {
		needs = append(needs, name)
	}
	filed, err := a.config.Operators.FileRequest(request.Context(), who, auth.OperatorRequest{Method: request.Method, Route: route.Method + " " + route.Pattern,
		Path: request.URL.Path, Body: stored, Summary: operatorRequestSummary(route, request.URL.Path, changedNames(shape.Set, shape.Unset, shape.Secrets)), Note: shape.Note, NeedsSecrets: needs})
	if err != nil {
		slog.Error("file operator request", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be stored.")
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"request_id": filed.ID, "status": filed.Status, "summary": filed.Summary,
		"expires_at": filed.ExpiresAt, "message": "Nothing changed yet: an operator approves or rejects this in the console."})
}

// withoutApproverFields drops what only the approving person gives: a phrase typed to confirm and an
// authenticator code. A token that filled them in (an injected prompt, say) gains nothing.
func withoutApproverFields(body []byte) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return body
	}
	delete(fields, "confirm")
	delete(fields, "reauth_code")
	cleaned, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return cleaned
}

// operatorRequestSummary says what a filed call would do, for the operator deciding.
func operatorRequestSummary(route Route, path string, keys []string) string {
	switch route.Method + " " + route.Pattern {
	case "POST /api/v1/operator/tokens":
		return "Create an operator token"
	case "PATCH /api/v1/operator/config":
		// The keys tell two requests apart in the list.
		if len(keys) > 3 {
			return "Change " + strings.Join(keys[:3], ", ") + " and " + strconv.Itoa(len(keys)-3) + " more"
		}
		if len(keys) > 0 {
			return "Change " + strings.Join(keys, ", ")
		}
		return "Change the server's settings"
	case "POST /api/v1/operator/config/versions/{id}/restore":
		return "Restore configuration version " + path[strings.LastIndex(strings.TrimSuffix(path, "/restore"), "/")+1:strings.LastIndex(path, "/")]
	case "POST /api/v1/operator/operators":
		return "Invite an operator"
	case "DELETE /api/v1/operator/operators/{id}":
		return "Revoke an operator"
	case "DELETE /api/v1/operator/sessions/{id}":
		return "End an operator session or token"
	}
	return route.Method + " " + path
}

type redeemOperatorRequest struct {
	Token string `json:"token"`
}

// signInStep answers where a sign-in stands; done sets the cookie.
func (a *API) signInStep(response http.ResponseWriter, step auth.SignInStep) {
	body := map[string]any{"step": step.Step, "email": step.Email}
	switch step.Step {
	case "done":
		a.setOperatorCookie(response, step.Session, int((12 * time.Hour).Seconds()))
		body["totp_required"] = a.hot().RequireTOTP
	case "code":
		body["ticket"] = step.Ticket
	case "enroll":
		body["ticket"] = step.Ticket
		body["key"] = auth.TOTPKeyText(step.Secret)
		body["otpauth_url"] = step.OTPAuthURL
		if svg, err := qrsvg.SVG(step.OTPAuthURL, "QR code of your authenticator key"); err == nil {
			body["qr_svg"] = svg
		} else {
			body["qr_svg"] = nil
		}
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, body)
}

// signInError answers a refused link, ticket or code, never saying more than the person needs.
func signInError(response http.ResponseWriter, step auth.SignInStep, left int, err error) {
	switch {
	case errors.Is(err, auth.ErrOperatorLocked) && step.LockedUntil != nil:
		writeJSON(response, http.StatusLocked, map[string]any{"error": map[string]any{"code": "operator_locked",
			"message": "Sign-in is locked for 15 minutes after ten wrong codes.", "locked_until": step.LockedUntil.UTC()}})
	case errors.Is(err, auth.ErrOperatorCode):
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "operator_code_wrong",
			"message": "That code didn't match.", "tries_left": left}})
	case errors.Is(err, auth.ErrOperatorLinkExpire):
		writeError(response, http.StatusGone, "operator_link_expired", "This link expired. Print a new one with collie operator pair.")
	case errors.Is(err, auth.ErrOperatorLinkUsed):
		writeError(response, http.StatusConflict, "operator_link_used", "This link was used already. Print a new one with collie operator pair.")
	case errors.Is(err, auth.ErrOperatorLink):
		writeError(response, http.StatusNotFound, "operator_link_invalid", "This link is not valid. Print a new one with collie operator pair.")
	case errors.Is(err, auth.ErrOperatorNoRing):
		writeError(response, http.StatusConflict, "secret_keys_missing", "An authenticator needs MAILBOX_TOKEN_KEYS on the server.")
	default:
		slog.Error("operator sign-in", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "Sign-in failed. Try again.")
	}
}

// tooManySignInTries limits each link and ticket to 5 tries a minute, and each client address to 60.
func (a *API) tooManySignInTries(response http.ResponseWriter, request *http.Request, secret string) bool {
	hash := store.RequestFingerprint(secret)
	now := a.now()
	byLink, _ := a.operatorSignIns.allow("link:"+hash, now)
	byAddress, _ := a.operatorSignInIPs.allow(clientIP(request, a.config.TrustedProxies), now)
	if byLink && byAddress {
		return false
	}
	response.Header().Set("Retry-After", "60")
	writeError(response, http.StatusTooManyRequests, "too_many_requests", "Too many tries. Wait a minute.")
	return true
}

func (a *API) redeemOperatorLink(response http.ResponseWriter, request *http.Request) {
	var input redeemOperatorRequest
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Token) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the token of the link.")
		return
	}
	if a.tooManySignInTries(response, request, input.Token) {
		return
	}
	step, err := a.config.Operators.Redeem(request.Context(), input.Token)
	if err != nil {
		signInError(response, step, 0, err)
		return
	}
	a.signInStep(response, step)
}

func (a *API) confirmOperatorCode(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Ticket) == "" || strings.TrimSpace(input.Code) == "" {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalid_request", "message": "Send the ticket and the code.",
			"fields": map[string]string{"code": "Enter the 6-digit code."}}})
		return
	}
	if a.tooManySignInTries(response, request, input.Ticket) {
		return
	}
	step, left, err := a.config.Operators.ConfirmTOTP(request.Context(), input.Ticket, input.Code)
	if err != nil {
		signInError(response, step, left, err)
		return
	}
	a.signInStep(response, step)
}

func operatorSessionJSON(session auth.OperatorSession) map[string]any {
	permissions := session.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return map[string]any{"id": session.ID, "operator_id": session.OperatorID, "email": session.Email, "kind": session.Kind, "label": session.Label,
		"permissions": permissions, "created_at": session.CreatedAt.UTC(), "last_seen_at": session.LastSeenAt.UTC(), "expires_at": session.ExpiresAt.UTC(), "revoked_at": session.RevokedAt}
}

func (a *API) getOperatorMe(response http.ResponseWriter, request *http.Request) {
	who := operatorPrincipal(request)
	permissions := who.Permissions
	if !who.IsToken() {
		permissions = auth.OperatorPermissions
	}
	session := auth.OperatorSession{ID: who.SessionID, OperatorID: who.Operator.ID, Email: who.Operator.Email, Kind: who.Kind, Permissions: permissions,
		CreatedAt: who.CreatedAt, LastSeenAt: a.now(), ExpiresAt: who.ExpiresAt}
	writeJSON(response, http.StatusOK, map[string]any{
		"operator": map[string]any{"id": who.Operator.ID, "email": who.Operator.Email, "totp_enabled": who.Operator.TOTPEnabledAt != nil},
		"session":  operatorSessionJSON(session),
		"console":  map[string]any{"origin": a.config.Operators.ConsoleBase(), "path_mode": a.config.OperatorOrigin == ""},
	})
}

func (a *API) operatorLogout(response http.ResponseWriter, request *http.Request) {
	who := operatorPrincipal(request)
	if err := a.config.Operators.Logout(request.Context(), who); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The session could not be ended.")
		return
	}
	if who.ViaCookie {
		a.setOperatorCookie(response, "", -1)
	}
	response.WriteHeader(http.StatusNoContent)
}

// reauthorized checks the operator's authenticator code before a dangerous change, and answers the
// refusal itself; nothing is saved on a refusal.
func (a *API) reauthorized(response http.ResponseWriter, request *http.Request, code string) bool {
	err := a.config.Operators.Reauth(request.Context(), operatorPrincipal(request), code)
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrOperatorReauthCLI):
		writeError(response, http.StatusForbidden, "reauth_needs_new_link", "Confirming needs a fresh sign-in: run collie operator pair and open the link. Nothing was saved.")
	case errors.Is(err, auth.ErrOperatorCode):
		writeError(response, http.StatusBadRequest, "reauth_code_invalid", "That code didn't match. Nothing was saved.")
	case errors.Is(err, auth.ErrOperatorLocked):
		writeError(response, http.StatusLocked, "operator_locked", "Sign-in is locked for 15 minutes after ten wrong codes. Nothing was saved.")
	case errors.Is(err, auth.ErrOperatorReauth):
		writeError(response, http.StatusBadRequest, "reauth_required", "Enter your authenticator code to confirm. Nothing was saved.")
	default:
		slog.Error("operator reauthentication", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The code could not be checked.")
	}
	return false
}

// operatorOnce runs create once per Idempotency-Key of the operator; a repeat answers 200 with what the
// first answered (without what it showed once).
func (a *API) operatorOnce(response http.ResponseWriter, request *http.Request, operation string, input []string, create func(context.Context) (shown any, kept any, err error)) {
	key, ok := idempotencyKey(request)
	if !ok {
		writeInvalidIdempotencyKey(response)
		return
	}
	who := operatorPrincipal(request)
	var shown any
	run := func(ctx context.Context) (any, error) {
		first, kept, err := create(ctx)
		shown = first
		return kept, err
	}
	if key == "" {
		if _, err := run(request.Context()); err != nil {
			operatorCreateError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, shown)
		return
	}
	stored, replayed, err := a.config.Operators.Once(request.Context(), who.Operator.ID, operation, key, []byte(store.RequestFingerprint(input...)), run)
	switch {
	case errors.Is(err, auth.ErrOperatorKeyReused):
		writeError(response, http.StatusUnprocessableEntity, "idempotency_key_reused", "This Idempotency-Key was already used for another request. Send a new key for a new one.")
	case errors.Is(err, auth.ErrOperatorKeyInFlight):
		writeError(response, http.StatusConflict, "request_in_progress", "The first request with this Idempotency-Key is still running.")
	case err != nil:
		operatorCreateError(response, err)
	case replayed:
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(stored)
	default:
		writeJSON(response, http.StatusCreated, shown)
	}
}

func operatorCreateError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serverconfig.ErrConflict):
		writeError(response, http.StatusConflict, "config_changed", "Someone saved another version since you read this one. Nothing was saved.")
		return
	case errors.Is(err, serverconfig.ErrTrialPending):
		writeError(response, http.StatusConflict, "trial_pending", "A trial change runs: confirm or revert it first. Nothing was saved.")
		return
	case errors.Is(err, serverconfig.ErrNoTrial):
		writeError(response, http.StatusBadRequest, "no_trial", "That version is not a trial that still runs.")
		return
	case errors.Is(err, serverconfig.ErrNoChange):
		writeError(response, http.StatusBadRequest, "nothing_to_save", "That version is what runs already.")
		return
	}
	var invalid *auth.InvalidTokenError
	if errors.As(err, &invalid) {
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Reason)
		return
	}
	slog.Error("operator change", "error", err)
	writeError(response, http.StatusInternalServerError, "internal_error", "The change could not be saved.")
}

func (a *API) createOperatorToken(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Label       string   `json:"label"`
		Permissions []string `json:"permissions"`
		ReauthCode  string   `json:"reauth_code"`
		Note        string   `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Label) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A token needs a name.")
		return
	}
	if !a.reauthorized(response, request, input.ReauthCode) {
		return
	}
	who := operatorPrincipal(request)
	a.operatorOnce(response, request, "operator-token", append([]string{input.Label}, input.Permissions...), func(ctx context.Context) (any, any, error) {
		raw, session, err := a.config.Operators.CreateToken(ctx, who, input.Label, input.Permissions)
		if err != nil {
			return nil, nil, err
		}
		kept := map[string]any{"session": operatorSessionJSON(session)}
		return map[string]any{"token": raw, "session": operatorSessionJSON(session)}, kept, nil
	})
}

// changedNames lists, sorted, the settings a filed configuration change names.
func changedNames(set map[string]any, unset []string, secrets map[string]any) []string {
	var keys []string
	for name := range set {
		keys = append(keys, name)
	}
	for name := range secrets {
		keys = append(keys, name)
	}
	keys = append(keys, unset...)
	slices.Sort(keys)
	return slices.Compact(keys)
}
