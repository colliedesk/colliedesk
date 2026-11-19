package mcpserver_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// A team that connects an AI app before any mail reached Colliedesk finds an empty inbox. The server's
// instructions must send the model to the setup steps and the email tools, so the person gets the
// address to forward to instead of a dead end.
func TestInstructionsGuideTheModelThroughAnEmptyWorkspace(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Empty Desk", "admin@empty-desk.example.com")
	instructions := e.connect(w.read, "").InitializeResult().Instructions

	for _, want := range []string{
		"no conversations",
		"colliedesk_setup_progress_get",
		"colliedesk_email_channel_get",
		"colliedesk_email_test_message_send",
		"colliedesk_email_forwarding_confirmation_get",
	} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions do not mention %q; a model facing an empty inbox has no next step:\n%s", want, instructions)
		}
	}
}

// A workspace that signed up from an AI app starts with conversations tagged sample. The model must know
// they are practice, so it never reports them as customers and still leads the person to real mail.
func TestInstructionsSaySampleConversationsArePractice(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Sample Desk", "admin@sample-desk.example.com")
	instructions := e.connect(w.read, "").InitializeResult().Instructions

	for _, want := range []string{"tagged sample", "not customers", "from anyone else is real mail"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions do not mention %q; a model would read the samples as real customers:\n%s", want, instructions)
		}
	}
}

// Every tool the instructions name must exist, so a renamed tool never leaves the model calling a
// name the server does not have.
func TestEveryToolTheInstructionsNameExists(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Named Tools Desk", "admin@named-tools.example.com")
	instructions := e.connect(w.read, "").InitializeResult().Instructions

	known := map[string]bool{}
	for _, spec := range mcpserver.Catalog() {
		known[spec.Name] = true
	}
	for _, name := range regexp.MustCompile(`colliedesk_[a-z_]+`).FindAllString(instructions, -1) {
		if !known[name] {
			t.Errorf("instructions name %s, which is not a tool", name)
		}
	}
}
