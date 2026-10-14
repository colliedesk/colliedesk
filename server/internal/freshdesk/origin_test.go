package freshdesk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func originLine(m domain.Message) string {
	if m.Origin == nil {
		return m.Kind + ":<no origin>"
	}
	return m.Kind + ":" + m.Origin.Kind + ":" + m.Origin.ExternalID + ":to=" + strings.Join(m.Origin.To, ",") + ":cc=" + strings.Join(m.Origin.CC, ",")
}

func TestSourceDetailsAreKeptAndForwardsAreNotReplies(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	data := store.New(env.pool)
	ctx := context.Background()
	timeline := func(ticket string) []string {
		items, err := data.ListMessages(ctx, env.workspaceID, env.target(t, "ticket", ticket))
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, item := range items {
			lines = append(lines, originLine(item))
		}
		return lines
	}
	want := map[string]string{
		"1": "incoming:description:1:to=:cc=accounts@globex.example | incoming:incoming:7001:to=support@acme.example:cc=accounts@globex.example | outgoing:reply:7002:to=carol@globex.example:cc= | note:private_note:7003:to=:cc=",
		"2": "incoming:description:2:to=:cc= | note:forward:7004:to=vendor@partner.example:cc=",
		"6": "outgoing:description:6:to=vendor@partner.example:cc= | note:public_note:7006:to=:cc=",
	}
	for ticket, expected := range want {
		if got := strings.Join(timeline(ticket), " | "); got != expected {
			t.Errorf("ticket %s timeline:\n got %s\nwant %s", ticket, got, expected)
		}
	}
	conversation, err := data.GetConversation(ctx, env.workspaceID, env.target(t, "ticket", "1"))
	if err != nil {
		t.Fatal(err)
	}
	origin := conversation.Origin
	if origin == nil || origin.System != "freshdesk" || origin.ExternalID != "1" || origin.Type == nil || *origin.Type != "Problem" ||
		origin.Source == nil || *origin.Source != "email" || origin.DueAt == nil || !origin.DueAt.Equal(time.Date(2025, 1, 18, 23, 30, 0, 0, time.UTC)) ||
		origin.FirstResponseDueAt == nil || strings.Join(origin.CC, ",") != "accounts@globex.example" {
		t.Fatalf("ticket 1 origin = %+v", origin)
	}
	forwarded, err := data.GetConversation(ctx, env.workspaceID, env.target(t, "ticket", "2"))
	if err != nil || forwarded.Origin == nil || strings.Join(forwarded.Origin.ForwardedTo, ",") != "vendor@partner.example" {
		t.Fatalf("ticket 2 origin = %+v %v", forwarded.Origin, err)
	}
	messages, err := data.ListMessages(ctx, env.workspaceID, conversation.ID)
	if err != nil || messages[0].BodyHTML == nil || !strings.Contains(*messages[0].BodyHTML, "<b>INV-7</b>") {
		t.Fatalf("original HTML not kept: %v", err)
	}
}
