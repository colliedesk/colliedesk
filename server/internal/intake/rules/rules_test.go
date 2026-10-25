package rules_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
)

// Each rule of the channel's rules, as the store applied them before they were a function: what a
// message is, on what basis, and by which rule.
func TestEachRuleDecidesWhatTheStoreDid(t *testing.T) {
	const c, d = "conversation-1", "conversation-2"
	cases := []struct {
		name  string
		facts rules.Facts
		want  rules.Decision
	}{
		{"cancel", rules.Facts{Cancel: true, Commanded: true}, rules.Decision{Action: rules.Ignore, Basis: rules.Match, Rule: rules.CommandCancel}},
		{"a person's conversation", rules.Facts{IntoConversation: c, Replay: true, Thread: rules.Thread{ConversationID: d}},
			rules.Decision{Action: rules.Attach, Basis: rules.Person, Rule: rules.IntoConversation, ConversationID: c}},
		{"chatter of a commands scope", rules.Facts{CommandsOnly: true}, rules.Decision{Action: rules.Archive, Basis: rules.Default, Rule: rules.ScopeArchive}},
		{"chatter that joins what is collected", rules.Facts{CommandsOnly: true, Joins: true, Thread: rules.Thread{ConversationID: c}},
			rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.ThreadKey, ConversationID: c}},
		{"chatter opened by a person", rules.Facts{CommandsOnly: true, Replay: true}, rules.Decision{Action: rules.Create, Basis: rules.Default, Rule: rules.NewThread}},
		{"a command in a commands scope", rules.Facts{CommandsOnly: true, Commanded: true, CommandAction: "open"},
			rules.Decision{Action: rules.Create, Basis: rules.Match, Rule: rules.CommandOpen}},
		{"collected without a command, review on", rules.Facts{Flushing: true, Review: true, Thread: rules.Thread{ConversationID: c}},
			rules.Decision{Action: rules.Review, Basis: rules.Default, Rule: rules.ReviewHold}},
		{"collected with a command, review on", rules.Facts{Flushing: true, Review: true, Commanded: true, CommandAction: "open"},
			rules.Decision{Action: rules.Create, Basis: rules.Match, Rule: rules.CommandOpen}},
		{"reopen names a conversation of the sender", rules.Facts{Commanded: true, CommandAction: "reopen", RefConversation: c, Thread: rules.Thread{ConversationID: d}},
			rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.CommandReopen, ConversationID: c}},
		{"reopen names none: the thread", rules.Facts{Commanded: true, CommandAction: "reopen", Thread: rules.Thread{ConversationID: d}},
			rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.ThreadKey, ConversationID: d}},
		{"a reference", rules.Facts{Thread: rules.Thread{ConversationID: c, ByReference: true}},
			rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.ThreadReference, ConversationID: c}},
		{"a thread key", rules.Facts{Thread: rules.Thread{ConversationID: c}}, rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.ThreadKey, ConversationID: c}},
		{"an open command whose thread matches joins it", rules.Facts{Commanded: true, CommandAction: "open", Thread: rules.Thread{ConversationID: c}},
			rules.Decision{Action: rules.Attach, Basis: rules.Match, Rule: rules.ThreadKey, ConversationID: c}},
		{"an open command with no thread", rules.Facts{Commanded: true, CommandAction: "open"}, rules.Decision{Action: rules.Create, Basis: rules.Match, Rule: rules.CommandOpen}},
		{"an open command the join policy refused", rules.Facts{Commanded: true, CommandAction: "open", Thread: rules.Thread{Refused: true}},
			rules.Decision{Action: rules.Create, Basis: rules.Match, Rule: rules.CommandOpen}},
		{"the join policy refused", rules.Facts{Thread: rules.Thread{Refused: true}}, rules.Decision{Action: rules.Create, Basis: rules.Default, Rule: rules.JoinPolicy}},
		{"an open command quoting another customer's message", rules.Facts{Commanded: true, CommandAction: "open", Thread: rules.Thread{OtherCustomer: true}},
			rules.Decision{Action: rules.Create, Basis: rules.Match, Rule: rules.CommandOpen}},
		{"another customer's message in a shared scope", rules.Facts{Thread: rules.Thread{OtherCustomer: true}},
			rules.Decision{Action: rules.Create, Basis: rules.Default, Rule: rules.ReferenceOtherCustomer}},
		{"nothing matches", rules.Facts{}, rules.Decision{Action: rules.Create, Basis: rules.Default, Rule: rules.NewThread}},
	}
	for _, c := range cases {
		if got := rules.Decide(c.facts); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
