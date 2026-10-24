package channels

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// OutMessage is the reply being sent.
type OutMessage struct {
	ID, Body  string
	CC        []string
	Files     []OutFile
	CreatedAt time.Time
}

// OutFile is a file of the reply in attachment storage.
type OutFile struct {
	Name, ContentType, StorageKey, SHA256 string
	Size                                  int64
}

// OutConversation is the conversation a reply belongs to.
type OutConversation struct {
	ID, Subject string
	// Origin is the type the conversation came through (conversations.channel).
	Origin Type
	// Opening is true when nothing came from the customer yet: the reply starts the thread.
	Opening bool
}

// Recipient is the customer a reply goes to.
type Recipient struct {
	ContactID, Name string
	// Email is empty for a contact known only by a channel identity.
	Email      string
	Identities []Identity
}

// Outbound is one reply as the frame hands it to the channel's sender.
type Outbound struct {
	DeliveryID, WorkspaceID string
	Channel                 Channel
	Message                 OutMessage
	Conversation            OutConversation
	Recipient               Recipient
	// References are the ids on this channel's type (without the type prefix) of the conversation's
	// earlier messages, oldest first; LastIncoming is the customer's newest of them.
	References   []string
	LastIncoming string
	// Prepared is what Prepare returned when the reply was queued.
	Prepared json.RawMessage
	Attempt  int
}

// PrepareRequest is a reply about to be queued, or a failed one about to be queued again.
type PrepareRequest struct {
	WorkspaceID    string
	Channel        Channel
	ConversationID string
	MessageID      string
	CC             []string
	Recipient      Recipient
	// Retry is set when an agent requeues a failed delivery.
	Retry bool
	// Host is the store queuing the reply, with the rules it runs under (outbound policy, plan
	// limits). An adapter that keeps its own tables asserts the interface it needs of it.
	Host any
}

// Delivery statuses as Prepare sets them.
const (
	DeliveryQueued = "queued"
	DeliveryFailed = "failed"
	DeliverySent   = "sent"
)

// Prepared is what the type decided when a reply was queued, in the transaction that stores it.
type Prepared struct {
	// Status is DeliveryQueued, or DeliveryFailed with Reason when the channel cannot send now.
	Status, Reason, Code string
	// ExternalID is the reply's id on the channel, fixed now so every retry carries the same one.
	ExternalID string
	// Recipients is how many people the reply reaches (the customer and its copies).
	Recipients int
	// Meta is kept with the delivery and given back to Send.
	Meta json.RawMessage
	// Transport fills the delivery's transport column, which only email's older images read.
	Transport string
}

// Send outcomes.
const (
	OutcomeSent = iota
	OutcomeFailed
	OutcomeDeferred
)

// SendResult is what one attempt to send did.
type SendResult struct {
	Outcome int
	// ExternalID is the id the channel gave the sent message, when it gives one.
	ExternalID string
	// Note is kept with a sent delivery that reached not everyone (a refused copy).
	Note string
	// Code is a fixed code (channel_off, reply_window_closed, quota) beside Reason, the words for people.
	Code, Reason string
	// Retryable makes a failure try again after a backoff; otherwise it waits for a person.
	Retryable bool
	// RetryAt is when a deferred delivery is due again; deferring spends no attempt.
	RetryAt time.Time
	// HeldByPause marks a deferral because the workspace's sending is paused.
	HeldByPause bool
	// Meta replaces what the delivery keeps for the next attempt, on every outcome: a reply sent in
	// parts records the parts that went, so a retry sends only the rest. Nil keeps it.
	Meta json.RawMessage
	// ExternalIDs are every id the channel gave what was sent (a reply sent in parts has several),
	// also on a failure after some parts went: the copies the channel sends back are known as ours.
	ExternalIDs []string
}

// Sent is a successful send.
func Sent(note string) SendResult { return SendResult{Outcome: OutcomeSent, Note: note} }

// Failed is a failure a person must look at.
func Failed(reason string) SendResult { return SendResult{Outcome: OutcomeFailed, Reason: reason} }

// Retry is a failure tried again after a backoff.
func Retry(reason string) SendResult {
	return SendResult{Outcome: OutcomeFailed, Reason: reason, Retryable: true}
}

// Defer waits until at without spending the attempt.
func Defer(at time.Time, reason string) SendResult {
	return SendResult{Outcome: OutcomeDeferred, RetryAt: at, Reason: reason}
}

// SendKit is what the outbox worker gives a sender.
type SendKit interface {
	Now() time.Time
	// Host is the store the worker runs on; see PrepareRequest.Host.
	Host() any
	// OpenFile reads a file of the reply from attachment storage.
	OpenFile(ctx context.Context, key string) (io.ReadCloser, error)
	// Credential opens a sealed secret of the channel, with its version.
	Credential(ctx context.Context, name string) (value string, version int64, found bool, err error)
	// ReplaceCredential stores a new value only while the stored version is still version (compare
	// and swap), so a token that may be used once is refreshed by one sender at a time.
	ReplaceCredential(ctx context.Context, name, value string, version int64, expiresAt *time.Time) (bool, error)
	// RefreshCredential refreshes a secret that may be used once: it locks the secret, and when its
	// version is still seen (no other sender refreshed it meanwhile) calls refresh with the current
	// value and stores what it returns. It returns the value and version in force afterwards.
	RefreshCredential(ctx context.Context, name string, seen int64, refresh Refresh) (value string, version int64, err error)
}

// Refresh exchanges a secret for its successor, with when the successor expires.
type Refresh func(ctx context.Context, current string) (next string, expiresAt *time.Time, err error)

// OutboundPart is the type's sender.
type OutboundPart interface {
	// Prepare runs in the transaction that stores the reply: it fixes ids that must survive retries,
	// applies the rules that refuse a reply (a trial's limits), and says whether it can be sent now.
	Prepare(ctx context.Context, tx pgx.Tx, request PrepareRequest) (Prepared, error)
	// Send runs in the outbox worker, outside any transaction.
	Send(ctx context.Context, out Outbound, kit SendKit) SendResult
}
