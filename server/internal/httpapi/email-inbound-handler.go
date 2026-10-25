package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	channelemail "github.com/colliedesk/colliedesk/server/internal/channels/email"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	inboundSecretHeader = "X-Colliedesk-Inbound-Secret"
	maxInboundBytes     = 25 << 20
	maxInboundFiles     = 100
)

// receiveInboundEmail accepts one raw message from a forwarder. Accepted outcomes are 200 so the
// forwarder does not retry; storage errors are 500 so it does, and nothing is lost silently.
func (a *API) receiveInboundEmail(response http.ResponseWriter, request *http.Request) {
	secret := request.Header.Get(inboundSecretHeader)
	platform := a.isPlatformSecret(secret)
	// A channel's own secret names the channel; the server-wide secret finds it from the recipient.
	var target inboundTarget
	if !platform {
		workspaceID, channelID, ok, err := a.store.ChannelForInboundSecret(request.Context(), secret)
		if err != nil {
			writeInternalError(response, err)
			return
		}
		if !ok {
			writeError(response, http.StatusUnauthorized, "unauthorized", "A valid inbound secret is required.")
			return
		}
		target = inboundTarget{workspaceID: workspaceID, channelID: channelID}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxInboundBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "The message is larger than 25 MiB.")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The message body could not be read.")
		return
	}
	parsed, err := email.Parse(raw, a.config.Email.TrustedAuthServID)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_message", "The body is not an RFC 5322 message with a sender.")
		return
	}
	if len(parsed.Files) > maxInboundFiles {
		writeError(response, http.StatusRequestEntityTooLarge, "too_many_files", fmt.Sprintf("The message has more than %d attachments.", maxInboundFiles))
		return
	}
	parsed.Platform = platform
	parsed.Cc = a.withoutPlatformAddresses(parsed.Cc)
	if platform {
		var hosted bool
		if target, hosted, err = a.platformTarget(request.Context(), parsed); err != nil {
			writeInternalError(response, err)
			return
		}
		if target.workspaceID == "" && hosted {
			writeError(response, http.StatusNotFound, "unknown_recipient", "No connected workspace receives mail at this address.")
			return
		}
		if target.workspaceID == "" {
			reason := "unknown_recipient"
			writeJSON(response, http.StatusOK, store.InboundResult{Status: "dropped", Reason: &reason, Authentication: parsed.Authentication})
			return
		}
	}
	if a.config.Attachments == nil {
		writeInternalError(response, errors.New("attachment storage is not configured; inbound mail cannot be kept"))
		return
	}
	channel, err := a.inboundChannel(request.Context(), target)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	results, err := a.pipeline.Receive(request.Context(), channel, []channels.Raw{{Parsed: parsed, Body: raw, ReceivedAt: a.now()}})
	if errors.Is(err, store.ErrRateLimited) {
		response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many messages from this sender. Retry later.")
		return
	}
	if errors.Is(err, domain.ErrMailboxClosed) {
		writeError(response, http.StatusForbidden, "mailbox_closed", "This mailbox is not accepting new mail.")
		return
	}
	if writePlanLimit(response, err) {
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	outcome := results[0]
	writeJSON(response, http.StatusOK, store.InboundResult{Status: outcome.Status, Reason: outcome.Reason, ConversationID: outcome.ConversationID,
		MessageID: outcome.MessageID, Authentication: parsed.Authentication})
}

// isPlatformSecret reports whether the secret is the server-wide secret of the MAIL_FROM_DOMAIN forwarder.
func (a *API) isPlatformSecret(secret string) bool {
	configured := a.config.Email.PlatformInboundSecret
	if configured == "" || secret == "" {
		return false
	}
	want, got := sha256.Sum256([]byte(configured)), sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// inboundTarget is where inbound mail goes: a workspace and, when the route knows it, its email channel.
type inboundTarget struct {
	workspaceID, channelID string
}

// inboundChannel reads the email channel mail for the target arrives on: the one named, else the
// workspace's default email channel.
func (a *API) inboundChannel(ctx context.Context, target inboundTarget) (channels.Channel, error) {
	if target.channelID != "" {
		return a.store.GetChannel(ctx, target.workspaceID, target.channelID)
	}
	channel, found, err := a.store.DefaultChannelOfType(ctx, target.workspaceID, channelemail.Type)
	if err == nil && !found {
		err = store.ErrEmailChannelMissing
	}
	return channel, err
}

// platformTarget routes mail that arrived with the server-wide secret. An envelope recipient at
// MAIL_FROM_DOMAIN is a reply address and picks the workspace of its conversation and the channel the
// conversation is answered from, even when that domain sits below HOSTED_MAIL_DOMAIN. Otherwise a
// recipient under HOSTED_MAIL_DOMAIN picks the workspace by its slug and the channel by its local part
// (hosted is true even when nothing matches, so the caller rejects instead of dropping), and a reply
// token among the recipients is the last resort.
func (a *API) platformTarget(ctx context.Context, parsed email.Inbound) (target inboundTarget, hosted bool, err error) {
	config := a.config.Email
	if email.ForDomain(parsed.EnvelopeTo, config.MailFromDomain) {
		target, err = a.replyTarget(ctx, []string{parsed.EnvelopeTo})
		return target, false, err
	}
	if email.UnderDomain(parsed.EnvelopeTo, config.HostedMailDomain) {
		// Only the envelope recipient counts: To and Cc are written by the sender.
		target, err = a.hostedTarget(ctx, parsed.EnvelopeTo)
		return target, true, err
	}
	target, err = a.replyTarget(ctx, parsed.Recipients)
	return target, false, err
}

// replyTarget returns the workspace of the conversation named by a reply+<token>@MAIL_FROM_DOMAIN
// recipient and the email channel its replies leave from; an empty target when there is none or no
// conversation has that token.
func (a *API) replyTarget(ctx context.Context, recipients []string) (inboundTarget, error) {
	token := email.PlatformReplyToken(recipients, a.config.Email.MailFromDomain)
	if token == "" {
		return inboundTarget{}, nil
	}
	workspaceID, conversationID, found, err := a.store.ConversationForReplyToken(ctx, token)
	if err != nil || !found {
		return inboundTarget{}, err
	}
	route, err := a.store.ReplyRoute(ctx, workspaceID, conversationID)
	if err != nil {
		return inboundTarget{}, err
	}
	target := inboundTarget{workspaceID: workspaceID}
	if route.Channel != nil && route.Channel.Type == channelemail.Type {
		target.channelID = route.Channel.ID
	}
	return target, nil
}

// hostedTarget returns the workspace and the email channel that receive mail at a hosted address, or
// an empty target when no workspace with a connected email channel owns it.
func (a *API) hostedTarget(ctx context.Context, recipient string) (inboundTarget, error) {
	slug := email.HostedSlug(recipient, a.config.Email.HostedMailDomain)
	if slug == "" {
		return inboundTarget{}, nil
	}
	workspaceID, ok, err := a.store.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !ok {
		return inboundTarget{}, err
	}
	// billing+invoice@ is billing@'s mailbox, as a +tag is for support addresses.
	channelID, found, err := a.store.ChannelForHostedAddress(ctx, workspaceID, email.LocalPart(email.MailboxKey(recipient)))
	if err != nil || !found {
		return inboundTarget{}, err
	}
	return inboundTarget{workspaceID: workspaceID, channelID: channelID}, nil
}

// checkInboundRecipient answers the hosted-cloud MTA before it accepts RCPT TO: 204 when inbound would
// route mail for this address, 404 when it would reject it. Refusing in the SMTP session means an
// unknown address is never bounced afterwards to a sender that may be forged.
func (a *API) checkInboundRecipient(response http.ResponseWriter, request *http.Request) {
	if !a.isPlatformSecret(request.Header.Get(inboundSecretHeader)) {
		writeError(response, http.StatusUnauthorized, "unauthorized", "The server-wide inbound secret is required.")
		return
	}
	var body struct {
		Recipient string `json:"recipient"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 4096)).Decode(&body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be {\"recipient\": \"<address>\"}.")
		return
	}
	var target inboundTarget
	var err error
	if email.ForDomain(body.Recipient, a.config.Email.MailFromDomain) {
		target, err = a.replyTarget(request.Context(), []string{body.Recipient})
	} else {
		target, err = a.hostedTarget(request.Context(), body.Recipient)
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if target.workspaceID == "" {
		writeError(response, http.StatusNotFound, "unknown_recipient", "No connected workspace receives mail at this address.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// withoutPlatformAddresses drops the addresses on the hosted forwarding and platform sending domains from
// a Cc list: they are ours, so they are never offered as people to copy.
func (a *API) withoutPlatformAddresses(addresses []string) []string {
	config := a.config.Email
	var kept []string
	for _, address := range addresses {
		if !email.UnderDomain(address, config.HostedMailDomain) && !email.UnderDomain(address, config.MailFromDomain) {
			kept = append(kept, address)
		}
	}
	return kept
}
