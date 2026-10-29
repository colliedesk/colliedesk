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

func TestOperatorConsoleServesItsScreensWithItsPolicy(t *testing.T) {
	for base, paths := range map[string][]string{"/operator": {"/operator", "/operator/sign-in", "/operator/config/email"}, "": {"/", "/sign-in", "/history"}} {
		handler := OperatorHandler(base)
		for _, path := range paths {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Header().Get("Content-Security-Policy") == "" || recorder.Header().Get("Referrer-Policy") != "no-referrer" || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s%s: headers %v", base, path, recorder.Header())
			}
			if recorder.Code == http.StatusOK && !strings.Contains(recorder.Body.String(), `name="colliedesk-operator-base" content="`+base+`"`) {
				t.Errorf("%s%s: the page does not name its base", base, path)
			}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, base+"/inbox", nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s/inbox: %d, want 404", base, recorder.Code)
		}
	}
}
