package mcpserver_test

import "testing"

func TestLookupToolsReturnNamesNotJustIDs(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")

	tags := e.ok(session, "colliedesk_tags_list", nil)
	if first := at(t, tags, "tags", 0); at(t, first, "name") != "billing" || at(t, first, "id") == "" {
		t.Errorf("tags=%v", tags)
	}
	agents := e.ok(session, "colliedesk_agents_list", nil)
	ada := at(t, agents, "agents", 0)
	if at(t, ada, "name") != "Ada Admin" || at(t, ada, "email") != "admin@inbox.example.com" || at(t, ada, "role") != "admin" || at(t, ada, "id") != f.adminID {
		t.Errorf("agents=%v", agents)
	}

	found := e.ok(session, "colliedesk_contacts_find", map[string]any{"query": "GRACE"})
	if at(t, found, "total_matches") != float64(1) || at(t, found, "contacts", 0, "untrusted_customer_content", "email") != "grace@cust.example.com" {
		t.Errorf("contacts_find GRACE=%v", found)
	}
	byMail := e.ok(session, "colliedesk_contacts_find", map[string]any{"query": "@cust.example.com", "limit": 1})
	if at(t, byMail, "total_matches") != float64(2) || len(byMail["contacts"].([]any)) != 1 || at(t, byMail, "truncated") != true {
		t.Errorf("limit 1 of 2 matches: %v", byMail)
	}
	if message := e.fails(session, "colliedesk_contacts_find", map[string]any{"query": "g"}); message == "" {
		t.Error("a one-letter query must be refused so the whole table cannot be dumped")
	}
}
