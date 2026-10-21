package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

func TestRegisteredRoutesExistInOpenAPI(t *testing.T) {
	document := loadContract(t)
	for _, registered := range httpapi.Routes() {
		operation := document.Paths.Find(registered.Pattern).GetOperation(registered.Method)
		if operation == nil {
			t.Errorf("contract lacks %s %s", registered.Method, registered.Pattern)
			continue
		}
		if permission, ok := operation.Extensions["x-permission"].(string); !ok || permission != registered.Permission {
			t.Errorf("%s %s permission=%v, want %s", registered.Method, registered.Pattern, operation.Extensions["x-permission"], registered.Permission)
		}
	}
}

func TestOpenAPIDeclaresAuthorizationResponses(t *testing.T) {
	document := loadContract(t)
	for _, registered := range httpapi.Routes() {
		if registered.Permission == "public" {
			continue
		}
		operation := document.Paths.Find(registered.Pattern).GetOperation(registered.Method)
		if operation.Responses.Value("401") == nil {
			t.Errorf("%s %s lacks 401 response", registered.Method, registered.Pattern)
		}
		// 403 comes from session permissions and CSRF; secret-authenticated channel routes have neither.
		if registered.Method != http.MethodGet && registered.Permission != auth.PermissionChannelInbound && operation.Responses.Value("403") == nil {
			t.Errorf("%s %s lacks 403 response", registered.Method, registered.Pattern)
		}
	}
}

func TestAPIResponsesMatchOpenAPIContract(t *testing.T) {
	api, authService := testAPI(t)
	_, pairingURL, err := authService.CreateWorkspace(context.Background(), "Contract Desk", "admin@contract.example.com")
	if err != nil {
		t.Fatal(err)
	}
	redeemRequest := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairingURL)}, "", nil)
	validateContractRequest(t, redeemRequest)
	redeem := serve(api, redeemRequest)
	validateContractResponse(t, redeemRequest, redeem)

	var session struct {
		Token string `json:"token"`
	}
	decode(t, redeem, &session)
	meRequest := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, session.Token, nil)
	validateContractRequest(t, meRequest)
	me := serve(api, meRequest)
	validateContractResponse(t, meRequest, me)
}

func init() {
	// Inbound email bodies are raw RFC 5322 bytes; validate them as an opaque string.
	openapi3filter.RegisterBodyDecoder("message/rfc822", openapi3filter.FileBodyDecoder)
	// Knowledge base images arrive as their raw bytes.
	for _, mediaType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		openapi3filter.RegisterBodyDecoder(mediaType, openapi3filter.FileBodyDecoder)
	}
}

// The contract is parsed and validated once per test binary: every request and response check reads
// it, and parsing it on each check made the contract tests the slowest of the package.
var contract = sync.OnceValues(func() (*openapi3.T, error) {
	path := filepath.Join("..", "..", "..", "contracts", "openapi.yaml")
	document, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI contract: %w", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate OpenAPI contract: %w", err)
	}
	return document, nil
})

var router = sync.OnceValues(func() (routers.Router, error) {
	document, err := contract()
	if err != nil {
		return nil, err
	}
	return legacy.NewRouter(document)
})

func contractRouter(t *testing.T) routers.Router {
	t.Helper()
	found, err := router()
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func loadContract(t *testing.T) *openapi3.T {
	t.Helper()
	document, err := contract()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func validateContractResponse(t *testing.T, request *http.Request, response *httptest.ResponseRecorder) {
	t.Helper()
	router := contractRouter(t)
	route, params, err := router.FindRoute(request)
	if err != nil {
		t.Fatalf("find contract route: %v", err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: request, PathParams: params, Route: route},
		Status:                 response.Code, Header: response.Header(), Body: response.Result().Body,
	}
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Fatalf("response violates contract: %v", err)
	}
}

func validateContractRequest(t *testing.T, request *http.Request) {
	t.Helper()
	router := contractRouter(t)
	route, params, err := router.FindRoute(request)
	if err != nil {
		t.Fatalf("find contract route: %v", err)
	}
	input := &openapi3filter.RequestValidationInput{
		Request: request, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
		t.Fatalf("request violates contract: %v", err)
	}
}
