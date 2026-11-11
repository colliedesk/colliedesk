package aiprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var pixel = []byte("\x89PNG\r\n\x1a\nfixture")

func imageRequest() Request {
	request := draftRequest()
	request.Messages = []Message{{Role: RoleUser, Text: "what is wrong here", Images: []Image{{MediaType: "image/png", Data: pixel}}}}
	return request
}

// An image goes to an OpenAI-compatible server as a content part with a data URL, after the text; a
// message without images keeps the plain text content every server reads.
func TestImagesGoToOpenAICompatibleServersAsDataURLs(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	if _, err := newTestOpenAI(t, fake, secretKey).Complete(context.Background(), imageRequest()); err != nil {
		t.Fatal(err)
	}
	messages := fake.recorded(t)[0].body["messages"].([]any)
	parts, ok := messages[1].(map[string]any)["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %v", messages[1])
	}
	text, image := parts[0].(map[string]any), parts[1].(map[string]any)
	url := image["image_url"].(map[string]any)["url"]
	if text["type"] != "text" || text["text"] != "what is wrong here" || image["type"] != "image_url" || url != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pixel) {
		t.Fatalf("parts = %v", parts)
	}
}

// An image goes to Claude as an image block with its bytes in base64.
func TestImagesGoToClaudeAsImageBlocks(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	if _, err := newTestAnthropic(t, fake, false).Complete(context.Background(), imageRequest()); err != nil {
		t.Fatal(err)
	}
	messages := fake.recorded(t)[0].body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %v", content)
	}
	image := content[1].(map[string]any)
	source := image["source"].(map[string]any)
	if image["type"] != "image" || source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != base64.StdEncoding.EncodeToString(pixel) {
		t.Fatalf("image = %v", image)
	}
}

// Only the four image types every provider takes, at most 4 a request and 5 MB each.
func TestOnlyImagesProvidersTakeAreSent(t *testing.T) {
	for name, images := range map[string][]Image{
		"svg":       {{MediaType: "image/svg+xml", Data: pixel}},
		"empty":     {{MediaType: "image/png"}},
		"too large": {{MediaType: "image/png", Data: make([]byte, MaxImageBytes+1)}},
		"five":      {{"image/png", pixel}, {"image/png", pixel}, {"image/png", pixel}, {"image/png", pixel}, {"image/png", pixel}},
	} {
		request := draftRequest()
		request.Messages[0].Images = images
		if err := request.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// openRouterFake answers GET /models with models and every completion with reply, and records the
// completions' bodies and how often the models were asked.
type openRouterFake struct {
	mu         sync.Mutex
	listStatus int
	listed     int
	bodies     []map[string]any
}

func newOpenRouter(t *testing.T, models, reply string, listStatus int) (Provider, *openRouterFake) {
	t.Helper()
	reasoningLists.Lock()
	reasoningLists.byEndpoint = map[string]*reasoningList{}
	reasoningLists.Unlock()
	fake := &openRouterFake{listStatus: listStatus}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			fake.listed++
			w.WriteHeader(fake.listStatus)
			_, _ = io.WriteString(w, models)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.bodies = append(fake.bodies, body)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: "https://openrouter.ai/api/v1", APIKey: "k",
		HTTPClient: &http.Client{Transport: &rewriteTransport{target: server.URL, next: server.Client().Transport}},
		Routing:    Routing{DataCollection: "deny", ZDR: true}})
	if err != nil {
		t.Fatal(err)
	}
	return provider, fake
}

const openRouterModels = `{"data":[
 {"id":"openai/gpt-5","supported_parameters":["reasoning","structured_outputs"],"reasoning":{"supported_efforts":["high","medium","low","minimal"]}},
 {"id":"anthropic/claude-sonnet-4.5","supported_parameters":["reasoning","structured_outputs"],"reasoning":{"mandatory":false}},
 {"id":"moonshotai/kimi","supported_parameters":["reasoning","structured_outputs"],"reasoning":{"supported_efforts":["max","high","none"]}},
 {"id":"meta-llama/llama-3.1-8b-instruct","supported_parameters":["structured_outputs"]}]}`

// OpenRouter gets the effort inside reasoning only for a model it lists as reasoning at that effort:
// with require_parameters, a model that does not would have no provider left. It routes by the
// provider's data policy, and reports what the call cost in its credits.
func TestOpenRouterGetsTheEffortOnlyForAModelThatReasonsAtIt(t *testing.T) {
	reply := strings.Replace(openAIReplyText, `"usage":{`, `"usage":{"cost":0.00123,"cost_details":{"upstream_inference_cost":null},`, 1)
	provider, fake := newOpenRouter(t, openRouterModels, reply, http.StatusOK)
	for model, want := range map[string]bool{
		"openai/gpt-5": true, "anthropic/claude-sonnet-4.5": true, "anthropic/claude-sonnet-4.5:nitro": true,
		"moonshotai/kimi": false, "meta-llama/llama-3.1-8b-instruct": false, "unlisted/model": false,
	} {
		request := draftRequest()
		request.Model = model
		response, err := provider.Complete(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		fake.mu.Lock()
		body := fake.bodies[len(fake.bodies)-1]
		fake.mu.Unlock()
		reasoning, _ := body["reasoning"].(map[string]any)
		route, _ := body["provider"].(map[string]any)
		if (reasoning["effort"] == "low") != want || (!want && body["reasoning"] != nil) || body["reasoning_effort"] != nil ||
			route["require_parameters"] != true || route["data_collection"] != "deny" || route["zdr"] != true {
			t.Fatalf("%s: body = %v", model, body)
		}
		if response.ProviderCost == nil || *response.ProviderCost != 0.00123 {
			t.Fatalf("cost = %v", response.ProviderCost)
		}
	}
	if fake.listed != 1 {
		t.Fatalf("the models were asked %d times, want once", fake.listed)
	}
}

// A model reads pictures when OpenRouter lists "image" among its inputs, a variant as its model does;
// without the list nothing does. The same list answers reasoning and pictures, asked once.
func TestOpenRouterSaysWhichModelsReadPictures(t *testing.T) {
	models := `{"data":[
 {"id":"google/gemini-2.5-flash","supported_parameters":["structured_outputs"],"architecture":{"input_modalities":["text","image","file"]}},
 {"id":"openai/gpt-5","supported_parameters":["reasoning","structured_outputs"],"architecture":{"input_modalities":["text"]}}]}`
	provider, fake := newOpenRouter(t, models, openAIReplyText, http.StatusOK)
	taker := provider.(ImageTaker)
	for model, want := range map[string]bool{"google/gemini-2.5-flash": true, "google/gemini-2.5-flash:floor": true, "openai/gpt-5": false, "unlisted/model": false} {
		if got := taker.TakesImages(context.Background(), model); got != want {
			t.Errorf("%s reads pictures = %v, want %v", model, got, want)
		}
	}
	request := draftRequest()
	request.Model = "openai/gpt-5"
	if _, err := provider.Complete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if fake.listed != 1 {
		t.Fatalf("the models were asked %d times, want once", fake.listed)
	}
	broken, _ := newOpenRouter(t, `{}`, openAIReplyText, http.StatusServiceUnavailable)
	if broken.(ImageTaker).TakesImages(context.Background(), "google/gemini-2.5-flash") {
		t.Fatal("without the list a model reads pictures")
	}
}

// When OpenRouter's model list cannot be read, the request goes without an effort, as before.
func TestOpenRouterWithoutItsModelListSendsNoEffort(t *testing.T) {
	provider, fake := newOpenRouter(t, `{}`, openAIReplyText, http.StatusServiceUnavailable)
	request := draftRequest()
	request.Model = "openai/gpt-5"
	if _, err := provider.Complete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if fake.bodies[0]["reasoning"] != nil {
		t.Fatalf("body = %v", fake.bodies[0])
	}
}

// Another server never gets OpenRouter's fields, and a reply without a cost has none.
func TestOtherServersGetNoOpenRouterFields(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: fake.server.Client(), Routing: Routing{DataCollection: "deny", ZDR: true}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	body := fake.recorded(t)[0].body
	if body["reasoning"] != nil || body["provider"] != nil || response.ProviderCost != nil {
		t.Fatalf("body = %v cost %v", body, response.ProviderCost)
	}
}
