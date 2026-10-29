// Package ai manages a workspace's AI providers and features: the server's provider (set by the
// operator), the providers a workspace adds with its own key, which provider and model each feature
// ("slot") uses, the history of those changes, and the usage row every provider call leaves. Calls
// to a provider go through Service.Metered and nowhere else. See docs/internals/ai.md.
package ai

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// Slots are the features a provider can be plugged into. Values are stored; they never change.
const (
	SlotTriage = "triage"
	SlotDraft  = "draft"
	// SlotChat answers end users in the help center's chat (ai-chat.go); it has its own money bucket.
	SlotChat = "chat"
)

// Slots lists every slot in display order.
var Slots = []string{SlotTriage, SlotDraft, SlotChat}

// DefaultServerLabel names the server's provider when AI_SERVER_LABEL is not set.
const DefaultServerLabel = "Server provider"

// ServerProvider is the provider the operator configures for every workspace of this server. On
// Colliedesk Cloud HC Labs pays for it; a self-hosted server uses it the same way.
type ServerProvider struct {
	Kind    aiprovider.Kind
	Label   string
	APIKey  string
	BaseURL string
	// Models lists, per slot, the models a workspace may pick; the first is the default. A slot
	// without models cannot use the server's provider.
	Models map[string][]string
	// Prices prices the models the provider bills (AI_SERVER_PRICES); Allowance, when set, bounds what
	// it spends per workspace and month (AI_SERVER_MONTHLY_ALLOWANCE). Without an allowance there is
	// no limit, as on a self-hosted server that pays its own provider.
	Prices    map[string]Price
	Allowance *Allowance
}

// ModelsFor returns the models the server offers for a slot.
func (s *ServerProvider) ModelsFor(slot string) []string {
	if s == nil {
		return nil
	}
	return s.Models[slot]
}

// ServerProviderFromEnv reads AI_SERVER_PROVIDER (anthropic or openai_compatible), AI_SERVER_API_KEY,
// AI_SERVER_BASE_URL, AI_SERVER_MODELS (triage=model|other,draft=model), AI_SERVER_LABEL,
// AI_SERVER_PRICES and AI_SERVER_MONTHLY_ALLOWANCE. None set means the server has no provider; an
// allowance needs a price for every model it offers. A partial setting is an error that names what is missing or
// wrong (never a value), so a half-configured server never starts.
func ServerProviderFromEnv(getenv func(string) string) (*ServerProvider, error) {
	value := func(key string) string { return strings.TrimSpace(getenv(key)) }
	kind, key, base, models, label := value("AI_SERVER_PROVIDER"), value("AI_SERVER_API_KEY"), value("AI_SERVER_BASE_URL"), value("AI_SERVER_MODELS"), value("AI_SERVER_LABEL")
	prices, allowance := value("AI_SERVER_PRICES"), value("AI_SERVER_MONTHLY_ALLOWANCE")
	if kind == "" && key == "" && base == "" && models == "" && label == "" && prices == "" && allowance == "" {
		return nil, nil
	}
	server := &ServerProvider{Kind: aiprovider.Kind(kind), Label: label, APIKey: key, BaseURL: base}
	if server.Label == "" {
		server.Label = DefaultServerLabel
	}
	var missing []string
	switch server.Kind {
	case aiprovider.KindAnthropic:
		if key == "" {
			missing = append(missing, "AI_SERVER_API_KEY")
		}
	case aiprovider.KindOpenAICompatible:
		if base == "" {
			missing = append(missing, "AI_SERVER_BASE_URL")
		}
	case "":
		missing = append(missing, "AI_SERVER_PROVIDER")
	default:
		return nil, fmt.Errorf("AI_SERVER_PROVIDER must be anthropic or openai_compatible")
	}
	if models == "" {
		missing = append(missing, "AI_SERVER_MODELS")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the server AI provider is half configured: set %s, or unset every AI_SERVER_ variable", strings.Join(missing, ", "))
	}
	if base != "" {
		parsed, err := url.Parse(base)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return nil, fmt.Errorf("AI_SERVER_BASE_URL must be an http or https URL without credentials")
		}
	}
	if len(label) > 80 {
		return nil, fmt.Errorf("AI_SERVER_LABEL must be at most 80 characters")
	}
	parsed, err := parseServerModels(models)
	if err != nil {
		return nil, err
	}
	server.Models = parsed
	if server.Prices, err = parsePrices(prices); err != nil {
		return nil, err
	}
	if server.Allowance, err = parseAllowance(allowance); err != nil {
		return nil, err
	}
	if server.Allowance != nil {
		var unpriced []string
		for _, slot := range Slots {
			for _, model := range server.Models[slot] {
				if _, ok := server.Prices[model]; !ok && !slices.Contains(unpriced, model) {
					unpriced = append(unpriced, model)
				}
			}
		}
		if len(unpriced) > 0 {
			return nil, fmt.Errorf("AI_SERVER_MONTHLY_ALLOWANCE needs a price in AI_SERVER_PRICES for %s", strings.Join(unpriced, ", "))
		}
	}
	return server, nil
}

func parseServerModels(value string) (map[string][]string, error) {
	models := map[string][]string{}
	for _, entry := range strings.Split(value, ",") {
		slot, list, ok := strings.Cut(strings.TrimSpace(entry), "=")
		slot = strings.TrimSpace(slot)
		if !ok || !slices.Contains(Slots, slot) {
			return nil, fmt.Errorf("AI_SERVER_MODELS must be written triage=model,draft=model (a slot may list more models separated by |)")
		}
		if _, seen := models[slot]; seen {
			return nil, fmt.Errorf("AI_SERVER_MODELS lists %s twice", slot)
		}
		for _, model := range strings.Split(list, "|") {
			model = strings.TrimSpace(model)
			if model == "" || len(model) > 200 {
				return nil, fmt.Errorf("AI_SERVER_MODELS has an empty or too long model for %s", slot)
			}
			if !slices.Contains(models[slot], model) {
				models[slot] = append(models[slot], model)
			}
		}
	}
	return models, nil
}
