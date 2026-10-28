package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// createWithoutKey lists the POST routes that create something and still take no Idempotency-Key, each
// with why a repeat of one press cannot create a second row. The list may only shrink: a new create route
// takes the key (contract first, then createOnce in its handler).
var createWithoutKey = map[string]string{
	"POST /api/v1/auth/cloud/signup":              "One trial per mailbox: a repeat of the same identity is refused, never a second workspace.",
	"POST /api/v1/contacts/{id}/merges":           "The duplicate merges once: a repeat finds it merged already and is refused.",
	"POST /api/v1/results/share-link":             "One link per agent: a new link replaces the old one, so repeats leave one link.",
	"POST /api/v1/export/link":                    "Stores no row: a short-lived signed link held in memory, to the same export.",
	"POST /api/v1/kb/images":                      "Stored by content: the same bytes again answer the image already stored.",
	"POST /api/v1/kb/media-uploads":               "An upload in progress holds no media; one left without parts expires, and the same bytes complete into the same item.",
	"POST /api/v1/kb/media-uploads/{id}/complete": "Acts on one upload by its id: a repeated complete answers the same item.",
	"POST /api/v1/billing/checkout":               "Opens a checkout session at the payment provider; nothing is stored here until it is paid.",
	"POST /api/v1/billing/portal":                 "Opens a portal session at the payment provider; nothing is stored here.",
	"POST /api/v1/conversations/{id}/ai-drafts":   "The person's earlier draft on the conversation is replaced: repeats leave one draft.",
	"POST /api/v1/conversations/{id}/ai-triage":   "The open suggestion is replaced: repeats leave one suggestion.",
}

// maxCreateWithoutKey is the ceiling of createWithoutKey; lower it when a route leaves the list.
const maxCreateWithoutKey = 11

// createsSomething reports whether an operation makes a new resource: it answers 201, or its name says it
// creates, adds or sends one.
func createsSomething(operation *openapi3.Operation) bool {
	if operation.Responses != nil && operation.Responses.Value("201") != nil {
		return true
	}
	for _, verb := range []string{"create", "add", "send"} {
		if strings.HasPrefix(operation.OperationID, verb) {
			return true
		}
	}
	return false
}

func takesIdempotencyKey(path *openapi3.PathItem, operation *openapi3.Operation) bool {
	for _, parameters := range []openapi3.Parameters{path.Parameters, operation.Parameters} {
		for _, parameter := range parameters {
			if parameter.Value != nil && parameter.Value.In == openapi3.ParameterInHeader && parameter.Value.Name == "Idempotency-Key" {
				return true
			}
		}
	}
	return false
}

// A press repeated by a double click, a slow network or a retry must create one row, not two: every POST
// route that creates something names the submit with an Idempotency-Key, or says here why it need not.
func TestEveryCreateRouteTakesAnIdempotencyKey(t *testing.T) {
	document := loadContract(t)
	seen := map[string]bool{}
	for _, route := range httpapi.Routes() {
		if route.Method != http.MethodPost {
			continue
		}
		key := route.Method + " " + route.Pattern
		if seen[key] {
			continue
		}
		seen[key] = true
		path := document.Paths.Find(route.Pattern)
		if path == nil || path.Post == nil {
			t.Errorf("%s is not in contracts/openapi.yaml", key)
			continue
		}
		creates, keyed := createsSomething(path.Post), takesIdempotencyKey(path, path.Post)
		_, exempt := createWithoutKey[key]
		switch {
		case creates && !keyed && !exempt:
			t.Errorf("%s (%s) creates something but takes no Idempotency-Key: add the CreateIdempotencyKey parameter, "+
				"the 200 repeat and the 422 idempotency_key_reused answer to the contract, and run the handler through createOnce",
				key, path.Post.OperationID)
		case exempt && (keyed || !creates):
			t.Errorf("createWithoutKey lists %s, which takes an Idempotency-Key or creates nothing: remove it and lower maxCreateWithoutKey", key)
		}
	}
	for key, why := range createWithoutKey {
		if !seen[key] {
			t.Errorf("createWithoutKey lists %s, which is not a POST route: remove it", key)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("createWithoutKey lists %s without saying why a repeat creates nothing twice", key)
		}
	}
	if len(createWithoutKey) > maxCreateWithoutKey {
		t.Errorf("createWithoutKey has %d routes, over the ceiling of %d: give the new create route an Idempotency-Key instead",
			len(createWithoutKey), maxCreateWithoutKey)
	}
}
