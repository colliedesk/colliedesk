package intake

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// In a group, another customer's words before a card may ask AI to set it aside: an ignore then waits for
// a person. The team's messages and the card's own customer do not count.
func TestAnIgnoreWaitsWhenAnotherCustomerWrote(t *testing.T) {
	a, b, agent := "contact-a", "contact-b", "agent-1"
	item := func(contact, agentID *string, role string) store.IntakeSnapshotItem {
		var it store.IntakeSnapshotItem
		it.ContactID, it.AgentID, it.Role = contact, agentID, role
		return it
	}
	card := []store.IntakeSnapshotItem{item(&a, nil, channels.RoleCustomer)}
	if otherCustomersWrote(store.IntakeSnapshot{Items: card, Before: []store.IntakeSnapshotItem{item(&a, nil, channels.RoleCustomer), item(nil, &agent, channels.RoleStaff)}}) {
		t.Fatal("the card's customer and the team counted as others")
	}
	if !otherCustomersWrote(store.IntakeSnapshot{Items: card, Before: []store.IntakeSnapshotItem{item(&b, nil, channels.RoleCustomer)}}) {
		t.Fatal("another customer before the card did not count")
	}
	if !otherCustomersWrote(store.IntakeSnapshot{Items: card, Quoted: map[string]store.IntakeSnapshotItem{"q": item(&b, nil, channels.RoleCustomer)}}) {
		t.Fatal("another customer's quoted message did not count")
	}
}
