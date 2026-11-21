package httpapi_test

import (
	"net/http"
	"testing"
)

// Each team member chooses the article editor they write in: none chosen means the web app opens the
// visual editor; the choice is theirs alone and can be changed back.
func TestKBEditorChoiceRoundTripsPerAgent(t *testing.T) {
	f := newTeamFixture(t)
	editor := func(token string) any {
		var me struct {
			Editor any `json:"kb_editor"`
		}
		response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
		decode(t, response, &me)
		return me.Editor
	}
	if editor(f.admin) != nil || editor(f.ann) != nil {
		t.Fatalf("no one chose yet: admin=%v ann=%v, want null", editor(f.admin), editor(f.ann))
	}
	put := func(token, value string) {
		t.Helper()
		req := newRequest(t, http.MethodPut, "/api/v1/agents/me/kb-editor", map[string]any{"editor": value}, token, nil)
		response := serve(f.api, req)
		if response.Code != http.StatusOK {
			t.Fatalf("PUT kb-editor status=%d body=%s", response.Code, response.Body.String())
		}
		validateContractResponse(t, req, response)
		var answer struct{ Editor string }
		decode(t, response, &answer)
		if answer.Editor != value {
			t.Fatalf("answer editor=%q, want %q", answer.Editor, value)
		}
	}
	put(f.ann, "markdown")
	if editor(f.ann) != "markdown" || editor(f.admin) != nil {
		t.Fatalf("the choice is per agent: ann=%v admin=%v, want markdown,null", editor(f.ann), editor(f.admin))
	}
	put(f.ann, "markdown")
	put(f.ann, "visual")
	if editor(f.ann) != "visual" {
		t.Fatalf("ann=%v, want visual", editor(f.ann))
	}
	for _, body := range []any{map[string]any{}, map[string]any{"editor": "wysiwyg"}, map[string]any{"editor": nil}, map[string]any{"editor": true}} {
		if response := request(t, f.api, http.MethodPut, "/api/v1/agents/me/kb-editor", body, f.ann, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v status=%d, want 400", body, response.Code)
		}
	}
}
