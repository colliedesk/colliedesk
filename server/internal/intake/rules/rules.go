// Package rules is how a channel's own rules decide what a message is, as a pure function of what the
// store read (docs/internals/channels.md "Rules decide first"): the frame's intake reads its decision,
// and an AI decider may stand in only where the rules decided by default, never where they matched.
// Nothing here names a channel type.
package rules

// Actions a decision takes.
const (
	// Ignore throws the message away with what was being collected with it (a cancel command).
	Ignore = "ignore"
	// Archive keeps the message without opening a conversation (chatter of a scope that opens one only on a command).
	Archive = "archive"
	// Review holds the message for a person to decide what it is.
	Review = "review"
	// Attach adds the message to a conversation, reopening it when resolved or closed.
	Attach = "attach"
	// Create opens a conversation.
	Create = "create"
)

// Bases of a decision. A match rests on evidence the message carries (a command, a reference, a thread
// key); a default is what the scope does with a message that carries none, which AI may replace.
const (
	Match   = "match"
	Default = "default"
	Person  = "person"
)

// Rules, as the journal names them (channel_inbound_events.decision_rule).
const (
	CommandCancel          = "command_cancel"
	CommandReopen          = "command_reopen"
	CommandOpen            = "command_open"
	ThreadReference        = "thread_reference"
	ThreadKey              = "thread_key"
	JoinPolicy             = "join_policy"
	NewThread              = "new_thread"
	ScopeArchive           = "scope_archive"
	ReviewHold             = "review_hold"
	IntoConversation       = "into_conversation"
	ReferenceOtherCustomer = "reference_other_customer"
)

// Thread is what looking up the conversation a message continues found.
type Thread struct {
	// ConversationID is the conversation it continues, empty when none.
	ConversationID string
	// ByReference says a reference to an earlier message matched it, not a thread key.
	ByReference bool
	// Refused says a conversation matched but the channel's join policy keeps the message out of it.
	Refused bool
	// OtherCustomer says the message refers to another customer's message in a scope several customers
	// share: a reply to someone else is no evidence of which request it belongs to.
	OtherCustomer bool
}

// Facts are what the rules read about a message, all of it read by the store beforehand.
type Facts struct {
	// Cancel: the message cancels what is being collected.
	Cancel bool
	// Commanded: it names one of the channel's commands; CommandAction is that command's action
	// (channels.ActionOpen or ActionReopen; a cancel is Cancel).
	Commanded     bool
	CommandAction string
	// IntoConversation is the conversation a person put a held message in.
	IntoConversation string
	// Replay: a person opened a held message.
	Replay bool
	// CommandsOnly: the scope opens a conversation only on a command; Joins says the message still belongs
	// to what is being collected for its sender or to an open conversation its thread finds.
	CommandsOnly bool
	Joins        bool
	// Flushing: collected messages are being stored as one; Review: the scope holds collected messages
	// that named no command for a person.
	Flushing bool
	Review   bool
	// RefConversation is the conversation of this sender a reopen command names, found.
	RefConversation string
	Thread          Thread
}

// Decision is what the rules decided, on what basis, by which rule.
type Decision struct {
	Action, Basis, Rule string
	// ConversationID is the conversation an Attach adds the message to.
	ConversationID string
}

// Decide applies the rules in their order: a cancel, a person's choice, a scope's chatter, a review,
// a reopen command, then the thread and the commands that open.
func Decide(f Facts) Decision {
	switch {
	case f.Cancel:
		return Decision{Action: Ignore, Basis: Match, Rule: CommandCancel}
	case f.IntoConversation != "":
		return Decision{Action: Attach, Basis: Person, Rule: IntoConversation, ConversationID: f.IntoConversation}
	case f.CommandsOnly && !f.Commanded && !f.Replay && !f.Joins:
		return Decision{Action: Archive, Basis: Default, Rule: ScopeArchive}
	case f.Flushing && f.Review && !f.Commanded:
		return Decision{Action: Review, Basis: Default, Rule: ReviewHold}
	case f.RefConversation != "":
		return Decision{Action: Attach, Basis: Match, Rule: CommandReopen, ConversationID: f.RefConversation}
	case f.Thread.OtherCustomer && f.Commanded:
		// A command is evidence of its own; the reply it quotes is not.
		return Decision{Action: Create, Basis: Match, Rule: CommandOpen}
	case f.Thread.OtherCustomer:
		return Decision{Action: Create, Basis: Default, Rule: ReferenceOtherCustomer}
	case f.Thread.ConversationID != "" && f.Thread.ByReference:
		return Decision{Action: Attach, Basis: Match, Rule: ThreadReference, ConversationID: f.Thread.ConversationID}
	case f.Thread.ConversationID != "":
		return Decision{Action: Attach, Basis: Match, Rule: ThreadKey, ConversationID: f.Thread.ConversationID}
	case f.Commanded:
		return Decision{Action: Create, Basis: Match, Rule: CommandOpen}
	case f.Thread.Refused:
		return Decision{Action: Create, Basis: Default, Rule: JoinPolicy}
	}
	return Decision{Action: Create, Basis: Default, Rule: NewThread}
}
