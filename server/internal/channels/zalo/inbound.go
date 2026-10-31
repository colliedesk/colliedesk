package zalo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
)

// event is a Zalo webhook delivery, the part of it this channel reads.
type event struct {
	AppID     string `json:"app_id"`
	OAID      string `json:"oa_id"`
	EventName string `json:"event_name"`
	Timestamp string `json:"timestamp"`
	Sender    struct {
		ID string `json:"id"`
	} `json:"sender"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		MsgID       string       `json:"msg_id"`
		Text        string       `json:"text"`
		Attachments []attachment `json:"attachments"`
	} `json:"message"`
}

type attachment struct {
	Type    string `json:"type"`
	Payload struct {
		URL       string `json:"url"`
		Thumbnail string `json:"thumbnail"`
		Name      string `json:"name"`
		Size      string `json:"size"`
		Type      string `json:"type"`
	} `json:"payload"`
}

// What a delivery is: a message a customer (or a team member on the Zalo app) sent the OA, alone or in a
// group, or a message the OA sent (a reply of ours, or the team writing in the OA's own chat).
func (e event) fromCustomer() bool { return strings.HasPrefix(e.EventName, "user_send_") }
func (e event) fromOA() bool       { return strings.HasPrefix(e.EventName, "oa_send_") }
func (e event) inGroup() bool {
	return strings.HasPrefix(e.EventName, "user_send_group_") || strings.HasPrefix(e.EventName, "oa_send_group_")
}

// isMessage says the delivery is a message; follows, reads and other notices are not.
func (e event) isMessage() bool { return e.fromCustomer() || e.fromOA() }

// oa is the Official Account a delivery belongs to: named in a group's events, the recipient of a
// customer's message, the sender of the OA's own.
func (e event) oa() string {
	switch {
	case e.OAID != "":
		return e.OAID
	case e.fromOA():
		return e.Sender.ID
	}
	return e.Recipient.ID
}

// id is the message's id on Zalo, or a stand-in made of what names the delivery when Zalo gave none.
func (e event) id() string {
	if e.Message.MsgID != "" {
		return e.Message.MsgID
	}
	return e.EventName + ":" + e.Sender.ID + ":" + e.Timestamp
}

func (e event) sentAt() time.Time {
	ms, err := strconv.ParseInt(e.Timestamp, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// kind is what the message carries after its prefix: text, image, file, video, sticker…
func (e event) kind() string {
	name := strings.TrimPrefix(strings.TrimPrefix(e.EventName, "user_send_"), "oa_send_")
	return strings.TrimPrefix(name, "group_")
}

func decode(raw json.RawMessage) (event, error) {
	var e event
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, fmt.Errorf("parse zalo event: %w", err)
	}
	return e, nil
}

type inbound struct{}

func (inbound) Mode() channels.IngestMode { return channels.Async }

// Files are the message's attachments that are files to keep (a picture, a file, a video), by link.
func (inbound) Files(raw channels.Raw) ([]channels.FileRef, error) {
	e, err := decode(raw.JSON)
	if err != nil {
		return nil, err
	}
	refs := []channels.FileRef{}
	for n, item := range e.Message.Attachments {
		if item.Payload.URL == "" {
			continue
		}
		ref := channels.FileRef{URL: item.Payload.URL, Name: item.Payload.Name}
		switch item.Type {
		case "image":
			ref.ContentType = "image/jpeg"
		case "video":
			ref.ContentType = "video/mp4"
		case "file":
			ref.ContentType = "application/octet-stream"
		default:
			continue
		}
		if ref.Name == "" {
			ref.Name = fmt.Sprintf("%s-%d", item.Type, n+1)
		}
		if size, err := strconv.ParseInt(item.Payload.Size, 10, 64); err == nil && size > 0 {
			ref.Size = size
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Parse makes one message of a delivery. A message the OA sent is the team's (the copy of a reply we
// sent is recognised by its id before it gets here); a group member who is one of the team's Zalo users is
// the team too. A kind of message this channel cannot show (a sticker, a location) is kept with a word for
// what it was, never dropped.
func (inbound) Parse(_ context.Context, channel channels.Channel, snap any, raw channels.Raw) ([]channels.Inbound, error) {
	e, err := decode(raw.JSON)
	if err != nil {
		return nil, err
	}
	if !e.isMessage() {
		return nil, nil
	}
	s, _ := snap.(snapshot)
	customer := e.Sender.ID
	if e.fromOA() {
		customer = e.Recipient.ID
	}
	in := channels.Inbound{ExternalID: e.id(), SentAt: e.sentAt(), Scope: ScopeDirect,
		Sender: channels.Sender{Identity: channels.Identity{Kind: IdentityUser, Value: customer}}}
	key := "user:" + customer
	in.Place = &channels.Place{Kind: channels.PlaceDirect, ExternalID: key, OpensConversations: true}
	if e.inGroup() {
		group := e.Recipient.ID
		if e.fromOA() {
			group = e.Recipient.ID
		}
		key, in.Scope = "group:"+group, ScopeGroup
		organization := s.groups[group]
		in.Sender.Organization = organization
		in.Place = &channels.Place{Kind: channels.PlaceGroup, ExternalID: key, Name: organization, OpensConversations: true}
	}
	in.Thread = channels.Thread{Keys: []string{key}, Record: []string{key}}
	meta := map[string]any{"via": "zalo", "event": e.EventName}
	switch {
	case e.fromOA():
		in.Role = channels.RoleStaff
		in.Sender.Identity = channels.Identity{Kind: "zalo_oa", Value: e.oa()}
	case s.staff[e.Sender.ID]:
		in.Role = channels.RoleStaff
	}
	in.Text = strings.TrimSpace(e.Message.Text)
	switch kind := e.kind(); kind {
	case "text", "image", "file", "video", "group_text":
	default:
		meta["kind"] = kind
		if in.Text == "" {
			in.Text = "[" + kind + "]"
		}
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
		meta["files_failed"] = failed
		in.Text = strings.TrimSpace(in.Text + "\n[file not received: " + strings.Join(failed, ", ") + "]")
	}
	if in.Text == "" {
		in.Text = "(no text)"
	}
	in.Meta, _ = json.Marshal(meta)
	if in.Role != channels.RoleStaff || e.fromCustomer() {
		if name, command, rest, ok := channels.MatchCommand(e.Message.Text, channel.Settings.Commands); ok {
			in.Intent = channels.Intent{Kind: channels.IntentCommand, Command: name}
			if command.Action == channels.ActionReopen {
				in.Intent.Ref = strings.TrimSpace(rest)
			}
		}
	}
	in.Subject = subject(in.Text)
	return []channels.Inbound{in}, nil
}

// subject is the first line of a message, as the conversation it opens is called.
func subject(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(strings.TrimSpace(line))
	if len(runes) > 80 {
		runes = runes[:80]
	}
	if len(runes) == 0 {
		return "Zalo message"
	}
	return string(runes)
}
