package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPAAndKeepsAPINamespace(t *testing.T) {
	api := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.NotFound(response, request)
	})
	handler := Handler(api)

	spa := httptest.NewRecorder()
	handler.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/conversations/123", nil))
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("SPA fallback status=%d body=%s", spa.Code, spa.Body.String())
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if apiResponse.Code != http.StatusNotFound {
		t.Fatalf("API status=%d, want 404", apiResponse.Code)
	}
}
