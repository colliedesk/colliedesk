package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// There is no way to delete a template for good before its 30 days in the trash, so the refusal at
// the limit names what the person can do now: edit a template they no longer need.
func TestATemplateOverTheLimitIsRefusedWithWhatToDoInstead(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "KB Template Limit", "admin@kb-template-limit.example.com")
	// The three starters count, and so does a template in the trash.
	deleteKBTemplate(t, api, token, kbTemplate(t, api, token, map[string]any{"name": "Trashed", "body": "gone"}))
	for index := 4; index < 200; index++ {
		kbTemplate(t, api, token, map[string]any{"name": fmt.Sprintf("Template %d", index), "body": "text"})
	}
	response := request(t, api, http.MethodPost, "/api/v1/kb/templates", map[string]any{"name": "One too many", "body": "text"}, token, nil)
	var refusal struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("status=%d body=%s: %v", response.Code, response.Body.String(), err)
	}
	want := "a workspace holds at most 200 templates, counting the ones in the trash for 30 days; edit one you no longer need instead."
	if response.Code != http.StatusConflict || refusal.Error.Code != "kb_template_limit" || refusal.Error.Message != want {
		t.Fatalf("status=%d code=%q message=%q, want 409 kb_template_limit %q", response.Code, refusal.Error.Code, refusal.Error.Message, want)
	}
}
