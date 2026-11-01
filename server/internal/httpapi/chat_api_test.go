package httpapi_test

import (
	"net/http"
	"testing"
)

// What customers said in a chat place is messages: a token reads it with messages:read, as it reads a
// conversation's messages; conversations:read alone files a request. Another workspace's channel is
// not there.
func TestChatRoutesNeedMessagesReadAndStayInTheirWorkspace(t *testing.T) {
	d := newLevelDesk(t)
	conversationsOnly := issueCustomToken(t, d.api, d.adminA, "conversations only", "conversations:read").Token
	messages := issueCustomToken(t, d.api, d.adminA, "messages", "messages:read").Token
	for _, path := range []string{"/api/v1/channels/" + d.chatA + "/threads", "/api/v1/channel-thread-items"} {
		if response := request(t, d.api, http.MethodGet, path, nil, conversationsOnly, nil); response.Code != http.StatusAccepted {
			t.Errorf("%s with conversations:read: %d %s", path, response.Code, response.Body.String())
		}
		if response := request(t, d.api, http.MethodGet, path, nil, messages, nil); response.Code != http.StatusOK {
			t.Errorf("%s with messages:read: %d %s", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/channels/" + d.chatA + "/threads", "/api/v1/channels/" + d.chatA + "/threads/00000000-0000-4000-8000-000000000000/items"} {
		if response := request(t, d.api, http.MethodGet, path, nil, d.adminB, nil); response.Code != http.StatusNotFound {
			t.Errorf("%s from B: %d", path, response.Code)
		}
	}
}
