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
	// Staff marks a team member writing on the channel's own app.
	Staff   bool `json:"staff,omitempty"`
	Archive bool `json:"archive,omitempty"`
	// Failed reports that our sent message with this id did not arrive.
	Failed string `json:"failed,omitempty"`
	// Links are files the channel serves by link, fetched by the frame.
	Links []LoopbackLink `json:"links,omitempty"`
}

// LoopbackLink is a file of a loopback message served by link.
type LoopbackLink struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
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
	// failPart makes the send of this part (counted from 1) fail once, after the parts before it went.
	failPart int
	// refreshes counts the token refreshes the endpoint granted.
	refreshes int
}

// FailPartOnce makes the next send fail at part n, after the parts before it went.
func (e *Endpoint) FailPartOnce(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failPart = n
}

// Refreshes counts the token refreshes granted.
func (e *Endpoint) Refreshes() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.refreshes
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
	// PartSize splits a longer reply into parts of this many bytes; zero sends one part.
	PartSize int
	// IntakeScopes are the scopes it declares for intake (each needs a Gather in Defaults.Scopes).
	IntakeScopes []string
	// ExtraScopes are declared after user and group.
	ExtraScopes []string
	// SharedScopes are the scopes it declares several customers write in.
	SharedScopes []string
}

// NewLoopback returns the Async loopback type with an endpoint.
func NewLoopback(kind channels.Type, mode channels.IngestMode) *Loopback {
	return &Loopback{Type: kind, Mode: mode, Endpoint: &Endpoint{}, Quiet: 10 * time.Second, Max: time.Minute,
		Defaults: channels.Settings{JoinPolicy: channels.JoinPolicy{Kind: channels.JoinWithinDays, Days: 7},
			RateLimits: channels.RateLimits{WindowSeconds: 600, PerSender: 20, NewConversations: 100, Scope: channels.ScopeChannel},
			Commands: map[string]channels.Command{"bug": {Phrases: []string{"Báo lỗi", "report bug"}, Tags: []string{"bug"}, Priority: "high"},
				"cancel": {Action: channels.ActionCancel, Phrases: []string{"Hủy"}},
				"reopen": {Action: channels.ActionReopen, Phrases: []string{"reopen"}}}}}
}

func (l *Loopback) Definition() channels.Definition {
	return channels.Definition{Type: l.Type, Identified: true, Name: "Loopback", Capabilities: []string{"threads", "attachments_in", "attachments_out"},
		ReplyTo: LoopbackIdentity, WhenOff: channels.OffHold, Defaults: l.Defaults, Scopes: append([]string{"user", "group"}, l.ExtraScopes...), IntakeScopes: l.IntakeScopes, SharedScopes: l.SharedScopes,
		FileLimits: channels.FileLimits{PerFile: 1000, PerEvent: 5000},
		Routes:     []channels.Route{{Method: http.MethodPost, Path: "/channels/" + string(l.Type) + "/webhook", Public: true, Handler: l.webhook}},
		HelpCenterRoutes: []channels.HelpCenterRoute{{Method: http.MethodGet, Path: "/" + string(l.Type), Handler: l.helpCenter},
			{Method: http.MethodPost, Path: "/" + string(l.Type) + "/{name}", Handler: l.helpCenter}},
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

// LoopbackHelpCenterAnswer is what loopback's help center route answers: where it was reached.
type LoopbackHelpCenterAnswer struct {
	WorkspaceID string `json:"workspace_id"`
	ChannelID   string `json:"channel_id"`
	Address     string `json:"address"`
	Base        string `json:"base"`
	Name        string `json:"name"`
}

// helpCenter is loopback's help center route: GET /<type> and POST /<type>/{name}, for the channel
// the query names (?channel=<id>) or the type's default; one that is off or not there is the help
// center's 404.
func (l *Loopback) helpCenter(kit channels.HelpCenterKit) channels.HelpCenterHandler {
	return func(w http.ResponseWriter, r *http.Request, at channels.HelpCenter) {
		channel, ok, err := kit.Channel(r.Context(), at.WorkspaceID, r.URL.Query().Get("channel"))
		if err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		if !ok || !channel.Enabled {
			at.NotFound(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(LoopbackHelpCenterAnswer{WorkspaceID: at.WorkspaceID, ChannelID: channel.ID, Address: at.Address, Base: at.Base,
			Name: r.PathValue("name")})
	}
}

// loopbackConfig keeps nothing of its own: the account is the channel's external account.
type loopbackConfig struct{}

func (loopbackConfig) Validate(_ context.Context, _ pgx.Tx, _ string, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
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

// Snapshot is the map of groups to organizations loopback keeps in the channel's settings
// (loopback_groups); a real type reads its own table.
func (loopbackConfig) Snapshot(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	groups := map[string]string{}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT settings->'loopback_groups' FROM channels WHERE workspace_id=$1 AND id=$2`, channel.WorkspaceID, channel.ID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

type loopbackInbound struct{ l *Loopback }

// Files lists the event's links for the frame to fetch.
func (i loopbackInbound) Files(raw channels.Raw) ([]channels.FileRef, error) {
	var event LoopbackEvent
	if err := json.Unmarshal(raw.JSON, &event); err != nil {
		return nil, err
	}
	refs := make([]channels.FileRef, len(event.Links))
	for n, link := range event.Links {
		refs[n] = channels.FileRef{URL: link.URL, Name: link.Name, ContentType: link.Type, Size: link.Size}
	}
	return refs, nil
}

func (i loopbackInbound) Mode() channels.IngestMode { return i.l.Mode }

func (i loopbackInbound) Parse(_ context.Context, channel channels.Channel, snapshot any, raw channels.Raw) ([]channels.Inbound, error) {
	var event LoopbackEvent
	if err := json.Unmarshal(raw.JSON, &event); err != nil {
		return nil, fmt.Errorf("parse loopback event: %w", err)
	}
	if event.From == "" {
		return nil, errors.New("a loopback event needs a sender")
	}
	key, scope := "user:"+event.From, "user"
	organization := ""
	if event.Group != "" {
		key, scope = "group:"+event.Group, "group"
		if groups, ok := snapshot.(map[string]string); ok {
			organization = groups[event.Group]
		}
	}
	in := channels.Inbound{ExternalID: event.ID, Sender: channels.Sender{Identity: channels.Identity{Kind: LoopbackIdentity, Value: event.From},
		Name: event.Name, Email: event.Email, Organization: organization}, Text: event.Text, SentAt: raw.ReceivedAt, Scope: scope,
		Thread: channels.Thread{Keys: []string{key}, Record: []string{key}}, Meta: json.RawMessage(`{"via":"loopback"}`)}
	if event.Reference != "" {
		in.Thread.References = []string{event.Reference}
	}
	in.Place = &channels.Place{Kind: channels.PlaceDirect, ExternalID: key, OpensConversations: true}
	if event.Group != "" {
		in.Place.Kind, in.Place.Name = channels.PlaceGroup, organization
	}
	if event.Staff {
		in.Role = channels.RoleStaff
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
	var failed []string
	for _, file := range raw.Files {
		if file.Object == nil {
			failed = append(failed, file.Name)
			continue
		}
		in.Files = append(in.Files, channels.InboundFile{Name: file.Name, ContentType: file.ContentType, Object: file.Object})
	}
	if len(failed) > 0 {
		in.Meta, _ = json.Marshal(map[string]any{"via": "loopback", "files_failed": failed})
		in.Text += "\n[file not received: " + strings.Join(failed, ", ") + "]"
	}
	switch {
	case event.Intent == "":
		if name, command, rest, ok := channels.MatchCommand(event.Text, channel.Settings.Commands); ok {
			in.Intent = channels.Intent{Kind: channels.IntentCommand, Command: name}
			if command.Action == channels.ActionReopen {
				in.Intent.Ref = strings.TrimSpace(rest)
			}
		}
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
	if len(endpoint.next) > 0 {
		result := endpoint.next[0]
		endpoint.next = endpoint.next[1:]
		endpoint.mu.Unlock()
		return result
	}
	endpoint.mu.Unlock()
	to := ""
	for _, identity := range out.Recipient.Identities {
		if identity.Kind == LoopbackIdentity {
			to = identity.Value
		}
	}
	token, version, _, err := kit.Credential(ctx, "token")
	if err != nil {
		return channels.Retry(err.Error())
	}
	if token == "expired" {
		token, _, err = kit.RefreshCredential(ctx, "token", version, func(context.Context, string) (string, *time.Time, error) {
			endpoint.mu.Lock()
			defer endpoint.mu.Unlock()
			endpoint.refreshes++
			return fmt.Sprintf("fresh-%d", endpoint.refreshes), nil, nil
		})
		if err != nil {
			return channels.Retry(err.Error())
		}
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
	parts := []string{out.Message.Body}
	if size := o.l.PartSize; size > 0 {
		parts = nil
		for body := out.Message.Body; body != ""; {
			n := min(size, len(body))
			parts, body = append(parts, body[:n]), body[n:]
		}
	}
	var progress struct {
		Sent int `json:"sent"`
	}
	_ = json.Unmarshal(out.Prepared, &progress)
	var ids []string
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	for i := progress.Sent; i < len(parts); i++ {
		if endpoint.failPart == i+1 {
			endpoint.failPart = 0
			meta, _ := json.Marshal(map[string]int{"sent": i})
			result := channels.Retry(fmt.Sprintf("part %d of %d did not go", i+1, len(parts)))
			result.Meta, result.ExternalIDs = meta, ids
			return result
		}
		id := fmt.Sprintf("part-%d-%s", i+1, out.Message.ID)
		ids = append(ids, id)
		endpoint.sent = append(endpoint.sent, Sent{ChannelID: out.Channel.ID, To: to, Text: parts[i], ExternalID: id,
			References: out.References, Files: files, Token: token})
	}
	meta, _ := json.Marshal(map[string]int{"sent": len(parts)})
	result := channels.Sent("")
	result.Meta, result.ExternalIDs = meta, ids
	return result
}
