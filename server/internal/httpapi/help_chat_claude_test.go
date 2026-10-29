package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// TestHelpChatAgainstClaude runs the chat's routes against the Claude API: a pre-warm (max_tokens 0 with
// thinking off), then a question whose call reads that cache back. It costs a cent, so it runs only by
// hand with the chat eval: AI_CHAT_EVAL=1 and ANTHROPIC_API_KEY (make eval-chat).
func TestHelpChatAgainstClaude(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if os.Getenv("AI_CHAT_EVAL") != "1" || key == "" {
		t.Skip("set AI_CHAT_EVAL=1 and ANTHROPIC_API_KEY to call Claude (make eval-chat)")
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.AI = ai.Config{Build: func(ai.Spec) (aiprovider.Provider, error) {
			return aiprovider.NewAnthropic(aiprovider.AnthropicConfig{APIKey: key})
		}}
		config.AI.Server = chatServerProvider()
	})
	d := newChatDeskOn(t, api, service, pool, nil, "Chat Claude")
	data, err := os.ReadFile("../ai/testdata/chat-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var eval struct {
		Articles []struct{ Title, Body string } `json:"articles"`
	}
	if err := json.Unmarshal(data, &eval); err != nil {
		t.Fatal(err)
	}
	collection := kbCollection(t, api, d.admin, map[string]any{"name": "ParcelPilot"})
	for _, a := range eval.Articles {
		kbArticle(t, api, d.admin, map[string]any{"collection_id": collection.ID, "title": a.Title, "status": "published", "body_md": a.Body})
	}
	if _, err := store.New(pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	if response := d.call(t, http.MethodPost, "/chat/prewarm", nil); response.Code != http.StatusNoContent {
		t.Fatalf("prewarm status=%d %s", response.Code, response.Body.String())
	}
	events := chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "How much is the Growth plan?"}))
	last := events[len(events)-1]
	if last.Type != "done" || last.Status != "answered" {
		t.Fatalf("events = %+v", events)
	}
	var warmWrote, turnRead int64
	if err := pool.QueryRow(context.Background(), `SELECT
			COALESCE((SELECT cache_write_tokens FROM ai_usage WHERE workspace_id=$1 AND feature='chat' ORDER BY started_at LIMIT 1),0),
			COALESCE((SELECT cache_read_tokens FROM ai_usage WHERE workspace_id=$1 AND feature='chat' ORDER BY started_at DESC LIMIT 1),0)`, d.ws).Scan(&warmWrote, &turnRead); err != nil {
		t.Fatal(err)
	}
	t.Logf("pre-warm wrote %d cached tokens; the question read %d back", warmWrote, turnRead)
	if warmWrote == 0 || turnRead == 0 {
		t.Fatal("the question did not read back what the pre-warm wrote")
	}
}
