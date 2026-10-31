package channels

import (
	"context"
	"encoding/json"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// IngestMode says when the pipeline processes what a channel receives.
type IngestMode int

const (
	// Sync processes in the request and answers with the result. The journal row is written in the
	// same transaction: the sender retries an error (a mail forwarder, a browser), so nothing is lost
	// and dropped or refused input writes nothing to storage.
	Sync IngestMode = iota
	// Async commits the raw event to the journal first and answers at once; a worker processes it.
	Async
)

// Raw is one raw event as a route received it.
type Raw struct {
	// ExternalEventID identifies the event on the channel; a second event with it is a duplicate.
	ExternalEventID string
	// JSON is a small payload kept in the journal (up to 256 KiB).
	JSON json.RawMessage
	// Body is a large payload (a raw email). It is kept only through the files Parse returns.
	Body []byte
	// Parsed is what the route already made of Body, so Parse need not parse it again (Sync only).
	Parsed any
	// ReceivedAt is when the route received it.
	ReceivedAt time.Time
	// Files are the event's files the frame fetched (MediaSource), in the order Files listed them.
	Files []FetchedFile
}

// FileRef is a file of a raw event the channel serves by link (links of chat media expire).
type FileRef struct {
	URL, Name, ContentType string
	// Size is what the channel says, zero when it says nothing; the limit is checked on the bytes.
	Size int64
}

// FetchedFile is a file the frame fetched for an event: in storage, or failed with why.
type FetchedFile struct {
	Index             int
	Name, ContentType string
	Object            *attachments.Object
	// Error is why it could not be fetched; the message then shows a placeholder.
	Error string
}

// MediaSource is the optional part of an InboundPart whose events carry files by link: the frame
// fetches them into storage right after the event is journaled, before it is parsed, held or
// collected, so a message opened days later still has them. Files is pure.
type MediaSource interface {
	Files(raw Raw) ([]FileRef, error)
}

// FileLimits bound what the frame fetches for a type; zero uses the frame's defaults.
type FileLimits struct {
	PerFile, PerEvent int64
}

// InboundKind says what an Inbound is.
type InboundKind int

const (
	// KindMessage is a customer's message.
	KindMessage InboundKind = iota
	// KindDeliveryUpdate reports on a reply the channel sent (a bounce).
	KindDeliveryUpdate
	// KindEcho is a copy of our own message the channel sends back; dropped.
	KindEcho
	// KindArchive is kept in the journal, and exported, without becoming a conversation (a group chat's chatter).
	KindArchive
	// KindIgnore is dropped with DropReason.
	KindIgnore
)

// Inbound is what an adapter's parser makes of one raw event. The pipeline is its only writer.
type Inbound struct {
	Kind InboundKind
	// ExternalID is the message's id on the channel; the pipeline stores "<type>:<id>" in
	// messages.external_id and finds duplicates and threads by it.
	ExternalID string
	Thread     Thread
	Sender     Sender
	// Subject of a conversation it opens.
	Subject string
	// Text is the message as stored (already defaulted when empty); HTML is optional.
	Text, HTML string
	Files      []InboundFile
	// SentAt is when the customer sent it; zero means now.
	SentAt time.Time
	// Meta is stored as messages.channel_meta.
	Meta json.RawMessage
	// EmailMeta is stored as messages.email_meta, the envelope the API shows of a message that
	// arrived by email or a form.
	EmailMeta *domain.IncomingEmail
	Intent    Intent
	// Aggregate collects messages before a conversation opens (Async only).
	Aggregate *Aggregate
	// Scope picks the rules of Settings.Scopes the message follows; empty follows the channel's.
	Scope string
	// Delivery is the report of a KindDeliveryUpdate; it may come with DropReason set.
	Delivery *DeliveryUpdate
	// DropReason drops the message (the channel's own address, an auto-reply) with this reason.
	DropReason string
	// StorageBound is the most storage the message can take; zero counts its files.
	StorageBound int64
	// Summary replaces the raw event in the journal of a Sync type (who sent what, without the bytes).
	Summary json.RawMessage
	// RateKey is what the rate limits count the message by, when not its sender's identity: a public
	// form whose sender types any address counts the place it came from instead.
	RateKey string
	// Place is where on a chat channel it was said (a one-to-one chat, a group); the store keeps every
	// message of a place as an item of its thread, whether or not it opens a conversation. Empty for a
	// type that has no places (email, a web form).
	Place *Place
	// Role is who wrote it: RoleCustomer (the default) or RoleStaff (a team member on the channel's app).
	Role string
}

// SenderKey is the key the journal and the rate limits record the message by.
func (in Inbound) SenderKey() string {
	if in.RateKey != "" {
		return in.RateKey
	}
	return in.Sender.Identity.Key()
}

// Roles of a message's writer.
const (
	RoleCustomer = "customer"
	RoleStaff    = "staff"
	RoleBot      = "bot"
)

// Place kinds.
const (
	PlaceDirect = "direct"
	PlaceGroup  = "group"
)

// Place is a place on a chat channel where messages are said.
type Place struct {
	// Kind is PlaceDirect or PlaceGroup.
	Kind string
	// ExternalID names it on the channel, stable across messages ("user:<id>", "group:<id>").
	ExternalID string
	// Name is a group's name, when the type knows it.
	Name string
	// OpensConversations says a message here may open a conversation.
	OpensConversations bool
}

// Thread says which conversation a message continues.
type Thread struct {
	// References are ids of earlier messages on the channel, in the order to try.
	References []string
	// Keys are thread keys of the type ("reply-token:<t>", "user:<id>"), in the order to try.
	Keys []string
	// Record are thread keys the conversation the message lands in gets, so later messages find it.
	Record []string
}

// IdentityEmail is the identity kind of an email address: it is the contact's email, not a channel identity.
const IdentityEmail = "email_address"

// Identity is who someone is on a channel.
type Identity struct {
	Kind, Value string
}

// Key is the identity as the journal and rate limits record it.
func (i Identity) Key() string { return i.Kind + ":" + i.Value }

// Sender is the person a message is from.
type Sender struct {
	Identity Identity
	Name     string
	// Email may be empty for an identity that is not an email address.
	Email        string
	Organization string
}

// InboundFile is a file of a message. The pipeline writes Data to storage only for a message it stores.
type InboundFile struct {
	Name, ContentType string
	Data              []byte
	// Object is a file already in storage.
	Object *attachments.Object
	Inline bool
	// Original is the raw event itself (an email's .eml.gz): the journal points at it, and an
	// EmailMeta records it as original_attachment_id.
	Original bool
}

// Intent kinds of a message.
const (
	IntentNone    = ""
	IntentCommand = "command"
	IntentConfirm = "confirm"
	IntentCancel  = "cancel"
)

// Intent is what the customer asked for in a message, as the adapter parsed it.
type Intent struct {
	Kind string
	// Command is the name of a command of Settings.Commands.
	Command string
	// Ref is what the customer named for a reopen (a conversation id).
	Ref string
}

// Aggregate collects the messages of one key until the sender is quiet for Quiet or Max passed.
type Aggregate struct {
	Key        string
	Quiet, Max time.Duration
}

// DeliveryUpdate reports that replies the channel sent failed.
type DeliveryUpdate struct {
	// References are the external ids (without the type prefix) of the replies it names.
	References []string
	// Recipients are the addresses that failed; when every one was only copied, the reply still
	// reached the customer and only a note is added.
	Recipients []string
	Reason     string
}

// IngestResult is what happened to one Inbound.
type IngestResult struct {
	Status         string  `json:"status"`
	Reason         *string `json:"reason"`
	ConversationID *string `json:"conversation_id"`
	MessageID      *string `json:"message_id"`
	EventID        string  `json:"-"`
}

// Result statuses.
const (
	ResultCreated   = "created"
	ResultThreaded  = "threaded"
	ResultDuplicate = "duplicate"
	ResultDropped   = "dropped"
	ResultHeld      = "held"
	ResultPending   = "pending"
	ResultAccepted  = "accepted"
	ResultArchived  = "archived"
)

// Drop reasons the frame gives.
const (
	DropOwnMessage     = "own_message"
	DropChannelRemoved = "channel_removed"
	DropEcho           = "echo"
	DropCancelled      = "cancelled"
	// DropConfirmed: a confirm command sent the request being written; it is no part of it.
	DropConfirmed = "confirmed"
	// DropExpired: a request left without its description past Gather.ExpireSeconds, and the message
	// that found it expired.
	DropExpired = "expired"
	// DropRefNotFound: a reopen command whose code names no conversation of the customer's.
	DropRefNotFound = "ref_not_found"
)

// Hold reasons.
const (
	HoldChannelOff = "channel_off"
	HoldReview     = "review"
	// HoldIntake keeps a chat's batch the rules decided only by default for intake to decide.
	HoldIntake = "intake"
)

// InboundPart is the type's input: raw events in, normalized messages out.
type InboundPart interface {
	Mode() IngestMode
	// Parse is pure: the same raw event and snapshot (ConfigPart.Snapshot) always give the same
	// messages, so an event processed again from the journal gives what it gave. It never writes.
	// One raw event may hold several messages.
	Parse(ctx context.Context, channel Channel, snapshot any, raw Raw) ([]Inbound, error)
}
