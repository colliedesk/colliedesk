package helpscout_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// An imported conversation keeps the mailbox it was sent to, so its replies leave from that address's
// email channel when the workspace has one.
func TestAnImportedConversationKeepsItsMailbox(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	conversation, err := store.New(env.pool).GetConversation(context.Background(), env.workspaceID, env.target(t, "ticket", "101"))
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Origin == nil || fmt.Sprint(conversation.Origin.Mailboxes) != "[support@acme.example]" {
		t.Fatalf("origin = %+v", conversation.Origin)
	}
}
