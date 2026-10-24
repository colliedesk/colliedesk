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
	// Delivery is the report of a KindDeliveryUpdate; it may come with DropReason set.
	Delivery *DeliveryUpdate
	// DropReason drops the message (the channel's own address, an auto-reply) with this reason.
	DropReason string
	// StorageBound is the most storage the message can take; zero counts its files.
	StorageBound int64
	// Summary replaces the raw event in the journal of a Sync type (who sent what, without the bytes).
	Summary json.RawMessage
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
)

// Hold reasons.
const (
	HoldChannelOff = "channel_off"
	HoldReview     = "review"
)

// InboundPart is the type's input: raw events in, normalized messages out.
type InboundPart interface {
	Mode() IngestMode
	// Parse is pure: it reads raw and the channel, never writes. One raw event may hold several messages.
	Parse(ctx context.Context, channel Channel, raw Raw) ([]Inbound, error)
}
