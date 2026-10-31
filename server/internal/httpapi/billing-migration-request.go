package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/email"
)

const (
	migrationRequestOperation = "migration_request"
	// supportAddress receives the Freshdesk migration requests.
	supportAddress = "support@colliedesk.com"
	// migrationRequestsPerHour is how many requests one workspace may send per hour.
	migrationRequestsPerHour = 5
	maxMigrationTimes        = 500
	freshdeskSuffix          = ".freshdesk.com"
	maxHostLabel             = 63
)

// normalizeFreshdeskAddress reads what an admin typed (name.freshdesk.com, with or without a scheme, a
// path, a query or capitals) and returns the lower-case host name, or false when it is not exactly one
// label under freshdesk.com. A port, a user name or any other host is refused, so the address in the
// email can only ever name a Freshdesk account.
func normalizeFreshdeskAddress(input string) (string, bool) {
	value := strings.TrimSpace(input)
	lower := strings.ToLower(value)
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(lower, scheme) {
			value = value[len(scheme):]
			break
		}
	}
	if end := strings.IndexAny(value, "/?#"); end >= 0 {
		value = value[:end]
	}
	host := strings.ToLower(value)
	label, found := strings.CutSuffix(host, freshdeskSuffix)
	if !found || label == "" || len(label) > maxHostLabel || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return "", false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return "", false
		}
	}
	return host, true
}

// createMigrationRequest emails Colliedesk support that an admin wants the workspace's Freshdesk data
// moved, then stores the request so Plan and billing can show when it was sent. Nothing is stored when
// the mail does not go out.
func (a *API) createMigrationRequest(response http.ResponseWriter, request *http.Request) {
	var input struct {
		FreshdeskAddress string `json:"freshdesk_address"`
		Times            string `json:"times"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be a JSON object with a freshdesk_address.")
		return
	}
	fields := map[string]string{}
	address, ok := normalizeFreshdeskAddress(input.FreshdeskAddress)
	if !ok {
		fields["freshdesk_address"] = "Use your Freshdesk address, like yourcompany.freshdesk.com."
	}
	times := oneLine(input.Times, math.MaxInt)
	if utf8.RuneCountInString(times) > maxMigrationTimes {
		fields["times"] = fmt.Sprintf("Keep the times under %d characters.", maxMigrationTimes)
	}
	if len(fields) > 0 {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "validation_failed", "message": "Check the highlighted fields.", "fields": fields})})
		return
	}
	if a.config.AlertMailer == nil || a.config.Email.MailFromDomain == "" {
		writeError(response, http.StatusServiceUnavailable, "mail_unavailable", "This server cannot send mail, so the request was not sent. Write to "+supportAddress+" instead.")
		return
	}
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, migrationRequestOperation, []string{address, times}, func(ctx context.Context) (any, error) {
		if allowed, wait := a.migrationRequests.allow(who.Workspace.ID, a.now()); !allowed {
			response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(response, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("You can send %d migration requests an hour. Try again later.", migrationRequestsPerHour))
			return nil, errAnswered
		}
		requestedAt := a.now().UTC().Truncate(time.Second)
		if err := a.sendMigrationRequestMail(ctx, who, address, times, requestedAt); err != nil {
			slog.Error("send migration request", "error", err, "workspace_id", who.Workspace.ID)
			writeError(response, http.StatusBadGateway, "mail_failed", "The request could not be sent. Try again.")
			return nil, errAnswered
		}
		if err := a.store.CreateMigrationRequest(ctx, who.Workspace.ID, who.Agent.ID, address, times, requestedAt); err != nil {
			return nil, err
		}
		return map[string]string{"freshdesk_address": address, "times": times, "requested_at": requestedAt.Format(time.RFC3339)}, nil
	})
}

// sendMigrationRequestMail writes the request to support with the admin's address as Reply-To. It names
// the workspace and the admin and carries no customer data. The send outlives the request, so a client
// that gives up does not drop it.
func (a *API) sendMigrationRequestMail(ctx context.Context, who auth.Principal, address, times string, requestedAt time.Time) error {
	domainName := a.config.Email.MailFromDomain
	from := "noreply@" + domainName
	messageID, err := email.NewMessageID(domainName)
	if err != nil {
		return fmt.Errorf("compose migration request: %w", err)
	}
	name := who.Agent.DisplayName
	if name == "" {
		name = who.Agent.Email
	}
	if times == "" {
		times = "none given"
	}
	plan := who.Access.Plan
	if plan == "" {
		plan = "none (no plan limits)"
	}
	var body strings.Builder
	fmt.Fprintf(&body, "An admin asked Colliedesk to move the workspace's Freshdesk data.\n\n")
	fmt.Fprintf(&body, "Workspace: %s\nSlug: %s\nWorkspace id: %s\nPlan: %s\n", oneLine(who.Workspace.Name, 200), who.Workspace.Slug, who.Workspace.ID, plan)
	fmt.Fprintf(&body, "Freshdesk address: %s\nTimes that suit them: %s\n", address, times)
	fmt.Fprintf(&body, "Admin: %s <%s>\nRequested at: %s\nServer: version %s, realm %s\n\nReply to this email to answer the admin.\n",
		oneLine(name, 200), who.Agent.Email, requestedAt.Format(time.RFC3339), a.version(), a.realm())
	message, err := email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: supportAddress,
		ReplyTo: who.Agent.Email, ReplyToName: oneLine(name, 200),
		Subject: "Freshdesk migration request: " + oneLine(who.Workspace.Name, 200), Body: body.String(), MessageID: messageID, Date: requestedAt, Opening: true})
	if err != nil {
		return fmt.Errorf("compose migration request: %w", err)
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertSendTimeout)
	defer cancel()
	return a.config.AlertMailer.Send(sendCtx, from, []string{supportAddress}, message)
}
