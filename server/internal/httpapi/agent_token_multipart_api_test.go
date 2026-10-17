package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// A message with files is the same action as one without: the tier gate and the token attribution
// must not depend on whether the body is JSON or multipart.
func TestMessageWithFilesIsGatedByTokenTierLikeJSON(t *testing.T) {
	f := newAttachFixture(t)
	read := issueToken(t, f.api, f.tokenA, "reader", "read").Token
	draft := issueToken(t, f.api, f.tokenA, "drafter", "draft")
	file := []upload{{name: "log.txt", contentType: "text/plain", data: []byte("printer log")}}

	// Below the tier a JSON call becomes an agent request; files cannot wait days for a person, so the
	// multipart call is refused with the way forward.
	refused := func(label, token, kind string) {
		t.Helper()
		response := f.post(t, f.conversationA, token, kind, file)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "files_need_approval") ||
			!strings.Contains(response.Body.String(), "attach the files after approval") {
			t.Errorf("%s: status=%d body=%s, want 403 files_need_approval", label, response.Code, response.Body.String())
		}
	}
	refused("read adds a note with a file", read, "note")
	// The kind is read after the files, so a token that may write no message is refused up front.
	refused("read replies with a file", read, "outgoing")
	refused("draft replies with a file", draft.Token, "outgoing")
	f.wantNothingStored(t)

	response := f.post(t, f.conversationA, draft.Token, "note", file)
	if response.Code != http.StatusCreated {
		t.Fatalf("draft note with a file: status=%d body=%s", response.Code, response.Body.String())
	}
	var note struct {
		AgentToken *struct {
			ID string `json:"id"`
		} `json:"agent_token"`
	}
	decode(t, response, &note)
	if note.AgentToken == nil || note.AgentToken.ID != draft.Session.ID {
		t.Errorf("note with a file must name the token that wrote it; agent_token = %+v", note.AgentToken)
	}
}
