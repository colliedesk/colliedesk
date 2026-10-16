package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The running build is public so a release can be cut from exactly the commit Cloud runs.
func TestConfigReportsRunningVersion(t *testing.T) {
	api, _, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Version = "edge-f7f0e0a72c9c" })
	var config struct {
		Version string `json:"version"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil), &config)
	if config.Version != "edge-f7f0e0a72c9c" {
		t.Fatalf("version=%q, want edge-f7f0e0a72c9c", config.Version)
	}
}
