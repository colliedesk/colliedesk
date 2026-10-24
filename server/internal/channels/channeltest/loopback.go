// Package channeltest holds the contract every channel type must pass (Run) and loopback, a type
// that exists only in tests: it carries messages between a test and the pipeline, so the parts of
// the frame no shipped type uses yet (identities without email, collecting, Async, secrets, notices,
// its own route) are proven before a real type needs them. Production code never imports it.
package channeltest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
)

// LoopbackIdentity is the identity kind of a loopback user.
const LoopbackIdentity = "loopback_user"

// LoopbackEvent is one message of the loopback webhook.
type LoopbackEvent struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Name string `json:"name,omitempty"`
	// Email makes the user known by email as well.
	Email string `json:"email,omitempty"`
	Text  string `json:"text"`
	// Group threads by a group instead of the user.
	Group string `json:"group,omitempty"`
	// Reference is the id of an earlier message this one answers.
	Reference string         `json:"reference,omitempty"`
	Files     []LoopbackFile `json:"files,omitempty"`
	// Intent is "confirm", "cancel" or "command:<name>".
	Intent  string `json:"intent,omitempty"`
	Collect bool   `json:"collect,omitempty"`
	Echo    bool   `json:"echo,omitempty"`
	Archive bool   `json:"archive,omitempty"`
	// Failed reports that our sent message with this id did not arrive.
	Failed string `json:"failed,omitempty"`
}

// LoopbackFile is a file of a loopback message, base64.
type LoopbackFile struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
}

// Sent is a reply the loopback sender handed to its endpoint.
type Sent struct {
	ChannelID, To, Text, ExternalID string
	References                      []string
	Files                           []string
	Token                           string
}

// Endpoint is the outside world of loopback: what it sent, and what the next sends do.
type Endpoint struct {
	mu   sync.Mutex
	sent []Sent
	// next results, used in order; empty sends successfully.
	next []channels.SendResult
}

// Script makes the next sends give these results in order.
func (e *Endpoint) Script(results ...channels.SendResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.next = append(e.next, results...)
}

// Sent lists what was sent.
func (e *Endpoint) Sent() []Sent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Sent(nil), e.sent...)
}

// Loopback is the test type. Mode chooses Async (journal first) or Sync.
type Loopback struct {
	Type     channels.Type
	Mode     channels.IngestMode
	Endpoint *Endpoint
	Defaults channels.Settings
	// Quiet and Max are the collecting windows of a message with Collect.
	Quiet, Max time.Duration
}

// NewLoopback returns the Async loopback type with an endpoint.
func NewLoopback(kind channels.Type, mode channels.IngestMode) *Loopback {
	return &Loopback{Type: kind, Mode: mode, Endpoint: &Endpoint{}, Quiet: 10 * time.Second, Max: time.Minute,
		Defaults: channels.Settings{JoinPolicy: channels.JoinPolicy{Kind: channels.JoinWithinDays, Days: 7},
			RateLimits: channels.RateLimits{WindowSeconds: 600, PerSender: 20, NewConversations: 100, Scope: channels.ScopeChannel},
			Commands:   map[string]channels.Command{"bug": {Tags: []string{"bug"}, Priority: "high"}}}}
}

func (l *Loopback) Definition() channels.Definition {
	return channels.Definition{Type: l.Type, Name: "Loopback", Capabilities: []string{"threads", "attachments_in", "attachments_out"},
		ReplyTo: LoopbackIdentity, WhenOff: channels.OffHold, Defaults: l.Defaults,
		Routes: []channels.Route{{Method: http.MethodPost, Path: "/channels/" + string(l.Type) + "/webhook", Public: true, Handler: l.webhook}},
		Tools: []channels.ToolSpec{{Name: "colliedesk_" + string(l.Type) + "_ping", Description: "Checks the loopback route.",
			Method: http.MethodPost, Path: "/channels/" + string(l.Type) + "/webhook", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
}

func (l *Loopback) Config() channels.ConfigPart     { return loopbackConfig{} }
func (l *Loopback) Inbound() channels.InboundPart   { return loopbackInbound{l} }
func (l *Loopback) Outbound() channels.OutboundPart { return loopbackOutbound{l} }

// webhook is loopback's own route: {"account": "...", "events": [...]}.
func (l *Loopback) webhook(kit channels.RouteKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Account string            `json:"account"`
			Events  []json.RawMessage `json:"events"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			kit.WriteError(w, http.StatusBadRequest, "invalid_request", "The body must be JSON.")
			return
		}
		channel, ok, err := kit.FindChannel(r.Context(), body.Account)
		if err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		if !ok {
			kit.WriteError(w, http.StatusNotFound, "not_found", "No channel is that account.")
			return
		}
		raws := make([]channels.Raw, 0, len(body.Events))
		for _, event := range body.Events {
			var head LoopbackEvent
			if err := json.Unmarshal(event, &head); err != nil {
				kit.WriteError(w, http.StatusBadRequest, "invalid_request", "An event is not a loopback message.")
				return
			}
			raws = append(raws, channels.Raw{ExternalEventID: head.ID, JSON: event})
		}
		results, err := kit.Receive(r.Context(), channel, raws)
		if err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}
}

// loopbackConfig keeps nothing of its own: the account is the channel's external account.
type loopbackConfig struct{}

func (loopbackConfig) Validate(_ context.Context, _ pgx.Tx, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
	var config struct {
		Account *string `json:"account"`
	}
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &config); err != nil {
			return channels.ConfigChange{}, &channels.FieldError{Message: "The configuration must be a JSON object."}
		}
	}
	if current == nil && (config.Account == nil || strings.TrimSpace(*config.Account) == "") {
		return channels.ConfigChange{}, &channels.FieldError{Fields: map[string]string{"account": "Enter the account."}, Message: "Check the account."}
	}
	if config.Account == nil {
		return channels.ConfigChange{}, nil
	}
	account := strings.TrimSpace(*config.Account)
	return channels.ConfigChange{Value: config, ExternalAccount: &account, Detail: map[string]any{"account": account}}, nil
}

func (loopbackConfig) Save(context.Context, pgx.Tx, channels.Channel, channels.ConfigChange) (channels.Event, error) {
	return channels.Event{}, nil
}

func (loopbackConfig) Public(_ context.Context, _ pgx.Tx, channel channels.Channel) (any, error) {
	return map[string]string{"account": channel.ExternalAccount}, nil
}

func (loopbackConfig) Summary(_ context.Context, _ pgx.Tx, channel channels.Channel) (string, error) {
	return channel.ExternalAccount, nil
}

func (loopbackConfig) Credentials() []channels.CredentialSpec {
	return []channels.CredentialSpec{{Name: "token", Label: "Access token"}}
}

func (loopbackConfig) Status(context.Context, pgx.Tx, channels.Channel) (channels.Status, error) {
	return channels.Status{State: channels.StateWorking}, nil
}

func (loopbackConfig) Remove(context.Context, pgx.Tx, channels.Channel) error { return nil }

type loopbackInbound struct{ l *Loopback }

func (i loopbackInbound) Mode() channels.IngestMode { return i.l.Mode }

func (i loopbackInbound) Parse(_ context.Context, _ channels.Channel, raw channels.Raw) ([]channels.Inbound, error) {
	var event LoopbackEvent
	if err := json.Unmarshal(raw.JSON, &event); err != nil {
		return nil, fmt.Errorf("parse loopback event: %w", err)
	}
	if event.From == "" {
		return nil, errors.New("a loopback event needs a sender")
	}
	key := "user:" + event.From
	if event.Group != "" {
		key = "group:" + event.Group
	}
	in := channels.Inbound{ExternalID: event.ID, Sender: channels.Sender{Identity: channels.Identity{Kind: LoopbackIdentity, Value: event.From},
		Name: event.Name, Email: event.Email}, Text: event.Text, SentAt: raw.ReceivedAt,
		Thread: channels.Thread{Keys: []string{key}, Record: []string{key}}, Meta: json.RawMessage(`{"via":"loopback"}`)}
	if event.Reference != "" {
		in.Thread.References = []string{event.Reference}
	}
	in.Subject = "Loopback: " + event.Text
	if len(in.Subject) > 80 {
		in.Subject = in.Subject[:80]
	}
	if strings.TrimSpace(in.Text) == "" {
		in.Text = "(no text)"
	}
	for _, file := range event.Files {
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			return nil, fmt.Errorf("decode loopback file: %w", err)
		}
		in.Files = append(in.Files, channels.InboundFile{Name: file.Name, ContentType: file.Type, Data: data})
	}
	switch {
	case event.Intent == channels.IntentConfirm || event.Intent == channels.IntentCancel:
		in.Intent.Kind = event.Intent
	case strings.HasPrefix(event.Intent, "command:"):
		in.Intent = channels.Intent{Kind: channels.IntentCommand, Command: strings.TrimPrefix(event.Intent, "command:")}
	}
	if event.Collect {
		in.Aggregate = &channels.Aggregate{Key: key, Quiet: i.l.Quiet, Max: i.l.Max}
	}
	switch {
	case event.Echo:
		in.Kind = channels.KindEcho
	case event.Archive:
		in.Kind = channels.KindArchive
	case event.Failed != "":
		in.Kind = channels.KindDeliveryUpdate
		in.Delivery = &channels.DeliveryUpdate{References: []string{event.Failed}, Reason: "The loopback endpoint reports the reply did not arrive."}
		in.ExternalID = ""
	}
	return []channels.Inbound{in}, nil
}

type loopbackOutbound struct{ l *Loopback }

func (loopbackOutbound) Prepare(_ context.Context, _ pgx.Tx, request channels.PrepareRequest) (channels.Prepared, error) {
	prepared := channels.Prepared{Status: channels.DeliveryQueued, Recipients: 1, Meta: json.RawMessage(`{"prepared":true}`)}
	if !request.Retry {
		prepared.ExternalID = "out-" + request.MessageID
	}
	return prepared, nil
}

func (o loopbackOutbound) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	endpoint := o.l.Endpoint
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	result := channels.Sent("")
	if len(endpoint.next) > 0 {
		result, endpoint.next = endpoint.next[0], endpoint.next[1:]
	}
	if result.Outcome != channels.OutcomeSent {
		return result
	}
	to := ""
	for _, identity := range out.Recipient.Identities {
		if identity.Kind == LoopbackIdentity {
			to = identity.Value
		}
	}
	token, _, _, err := kit.Credential(ctx, "token")
	if err != nil {
		return channels.Retry(err.Error())
	}
	files := make([]string, 0, len(out.Message.Files))
	for _, file := range out.Message.Files {
		body, err := kit.OpenFile(ctx, file.StorageKey)
		if err != nil {
			return channels.Failed(err.Error())
		}
		data, _ := io.ReadAll(body)
		body.Close()
		files = append(files, file.Name+"="+string(data))
	}
	external := "out-" + out.Message.ID
	endpoint.sent = append(endpoint.sent, Sent{ChannelID: out.Channel.ID, To: to, Text: out.Message.Body, ExternalID: external,
		References: out.References, Files: files, Token: token})
	result.ExternalID = "remote-" + out.Message.ID
	return result
}
