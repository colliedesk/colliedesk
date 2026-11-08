package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// While the operator switched the server's provider off, Settings names it as off (so a feature set to
// it can say so) and no feature can pick it; another workspace sees the same, and its own providers stay.
func TestAServerProviderSwitchedOffCannotBePicked(t *testing.T) {
	d := newLevelDesk(t, func(config *httpapi.Config) {
		config.AI.Server = &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "k",
			Models: map[string][]string{ai.SlotTriage: {"claude-haiku-5-5"}}}
		config.SwitchedOff = map[string]bool{serverconfig.FeatureAIServer: true}
	})
	for _, token := range []string{d.adminA, d.adminB} {
		response := request(t, d.api, http.MethodGet, "/api/v1/ai", nil, token, nil)
		var settings struct {
			Server    *struct{ Label string } `json:"server"`
			ServerOff *struct{ Label string } `json:"server_off"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &settings) != nil {
			t.Fatalf("settings: %d %s", response.Code, response.Body.String())
		}
		if settings.Server != nil || settings.ServerOff == nil || settings.ServerOff.Label != "Colliedesk AI" {
			t.Fatalf("settings while the server's provider is off: %s", response.Body.String())
		}
	}
	d.setSlot(t, d.adminA, "triage", map[string]any{"source": "server", "enabled": true}, http.StatusConflict)
	d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true}, http.StatusOK)
}
