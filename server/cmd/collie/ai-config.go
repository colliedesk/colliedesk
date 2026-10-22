package main

import (
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/aidraft"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// aiServer is the AI provider the operator set up with AI_SERVER_* (docs/self-host/ai.md). On a
// self-hosted server it is the customer's own key; on Colliedesk Cloud the operator's.
type aiServer struct {
	Provider aiprovider.Provider
	Settings aidraft.Settings
}

// Enabled says whether a provider is set up; without one AI drafts are off and nothing is queued.
func (s aiServer) Enabled() bool { return s.Provider != nil }

// aiServerFromEnv reads AI_SERVER_PROVIDER (anthropic or openai_compatible), AI_SERVER_API_KEY,
// AI_SERVER_BASE_URL, AI_SERVER_MODEL, AI_SERVER_SELECT_MODEL and AI_SERVER_EFFORT. No provider is
// not an error. Errors name the variable, never its value.
func aiServerFromEnv(getenv func(string) string) (aiServer, error) {
	kind := strings.TrimSpace(getenv("AI_SERVER_PROVIDER"))
	if kind == "" {
		return aiServer{}, nil
	}
	settings := aidraft.Settings{Model: strings.TrimSpace(getenv("AI_SERVER_MODEL")), SelectModel: strings.TrimSpace(getenv("AI_SERVER_SELECT_MODEL")),
		Effort: aiprovider.Effort(strings.TrimSpace(getenv("AI_SERVER_EFFORT")))}
	if settings.Effort == "" {
		settings.Effort = aiprovider.EffortLow
	}
	if !settings.Effort.Valid() {
		return aiServer{}, fmt.Errorf("AI_SERVER_EFFORT must be low, medium or high")
	}
	key, base := strings.TrimSpace(getenv("AI_SERVER_API_KEY")), strings.TrimSpace(getenv("AI_SERVER_BASE_URL"))
	// The operator chose this endpoint, so it may be a private address (a local model server).
	client := aiprovider.NewHTTPClient(aiprovider.HTTPOptions{})
	var provider aiprovider.Provider
	var err error
	switch aiprovider.Kind(kind) {
	case aiprovider.KindAnthropic:
		if key == "" {
			return aiServer{}, fmt.Errorf("AI_SERVER_API_KEY is required with AI_SERVER_PROVIDER=anthropic")
		}
		if settings.Model == "" {
			settings.Model = "claude-sonnet-5-5"
		}
		if settings.SelectModel == "" {
			settings.SelectModel = "claude-haiku-5-5"
		}
		provider, err = aiprovider.NewAnthropic(aiprovider.AnthropicConfig{APIKey: key, BaseURL: base, HTTPClient: client})
	case aiprovider.KindOpenAICompatible:
		if settings.Model == "" {
			return aiServer{}, fmt.Errorf("AI_SERVER_MODEL is required with AI_SERVER_PROVIDER=openai_compatible")
		}
		provider, err = aiprovider.NewOpenAICompatible(aiprovider.OpenAIConfig{BaseURL: base, APIKey: key, HTTPClient: client})
	default:
		return aiServer{}, fmt.Errorf("AI_SERVER_PROVIDER must be anthropic or openai_compatible")
	}
	if err != nil {
		return aiServer{}, fmt.Errorf("AI_SERVER_BASE_URL or AI_SERVER_API_KEY is not usable: %w", err)
	}
	return aiServer{Provider: provider, Settings: settings}, nil
}
