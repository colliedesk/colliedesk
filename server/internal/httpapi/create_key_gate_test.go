package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// postWithoutKey lists the POST routes that take no Idempotency-Key, each with why a repeat of one press
// cannot store a second row. Every other POST route takes the key: a status code or an operation name does
// not tell a create from an action (an accepted 202 job, a stored form submission), so the rule is the
// route itself. The list may only shrink: a new POST route takes the key (contract first, then createOnce
// in its handler) or says here, precisely, why a repeat stores nothing twice.
var postWithoutKey = map[string]string{
	// Signing in and out.
	"POST /api/v1/auth/cloud/exchange": "A sign-in: it answers a session for a verified identity, which expires on its own; no workspace data is written.",
	"POST /api/v1/auth/cloud/signup":   "One trial per mailbox: a repeat of the same identity is refused, never a second workspace.",
	"POST /api/v1/auth/redeem":         "A one-time link: the first redeem spends it, a repeat is refused.",
	"POST /api/v1/auth/logout":         "Ends the caller's session: a repeat finds it ended.",
	// Actions on one row named by its id: a repeat finds the row already moved and changes nothing or is refused.
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry":       "Requeues the one failed delivery of a reply: only a failed delivery can be retried, so a repeat finds it queued (409).",
	"POST /api/v1/contacts/{id}/merges":                                "The duplicate merges once: a repeat finds it merged already and is refused.",
	"POST /api/v1/saved-views/{id}/restore":                            "Brings one view out of the trash: a view not in the trash is answered unchanged.",
	"POST /api/v1/saved-replies/{id}/restore":                          "Brings one reply out of the trash: a reply not in the trash is answered unchanged.",
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore":       "Carries the updated_at it read: the version it adds changes updated_at, so a repeat is refused (409 changed_elsewhere).",
	"POST /api/v1/imports/{id}/start":                                  "Starts one previewed job: a started job is no longer previewed, so a repeat is refused (409 import_state).",
	"POST /api/v1/imports/{id}/cancel":                                 "Cancels one job: a repeat finds it cancelled.",
	"POST /api/v1/imports/{id}/undo":                                   "Queues the undo of one import: while it is queued or running a repeat is refused (409 import_running).",
	"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/discard":      "Discards one draft: a repeat finds it discarded.",
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/accept":  "Applies one suggestion: a part already applied is not applied again.",
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss": "Dismisses one suggestion: a repeat finds it dismissed.",
	"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/undo":    "Takes back what AI applied: undoing again changes nothing.",
	"POST /api/v1/agent-requests/{id}/approve":                         "Decides one request: a decided request is refused (409), so its call runs once.",
	"POST /api/v1/agent-requests/{id}/reject":                          "Decides one request: a decided request is refused (409).",
	"POST /api/v1/agent-requests/{id}/cancel":                          "Cancels one request: a decided request is refused (409).",
	"POST /api/v1/kb/collections/{id}/restore":                         "Brings one collection out of the trash: a repeat finds it restored.",
	"POST /api/v1/kb/articles/{id}/restore":                            "Brings one article out of the trash: a repeat finds it restored.",
	"POST /api/v1/kb/articles/{id}/ai-exclusion":                       "Sets one flag on an article: excluding an excluded article changes nothing.",
	"POST /api/v1/kb/articles/{id}/versions/{version}/restore":         "Carries the updated_at it read: the version it adds changes updated_at, so a repeat is refused (409 changed_elsewhere).",
	"POST /api/v1/kb/media/{id}/restore":                               "Brings one item out of the trash: a repeat finds it restored.",
	"POST /api/v1/kb/media/{id}/replace":                               "Replaces the item where articles show it: after the first, no article shows it, so a repeat changes no text.",
	"POST /api/v1/kb/media-replacements/{id}/undo":                     "Undoes one replacement: undoing twice changes nothing more.",
	"POST /api/v1/kb/media/trash":                                      "Moves the named items to the trash: a repeat finds them there.",
	"POST /api/v1/kb/media/trash-unused":                               "Moves the unused items to the trash: a repeat finds none left unused outside it.",
	"POST /api/v1/kb/media/restore":                                    "Brings the named items out of the trash: a repeat finds them restored.",
	"POST /api/v1/kb/media/purge":                                      "Deletes the named items in the trash: a repeat answers not_found for each.",
	"POST /api/v1/kb/media-uploads/{id}/complete":                      "Acts on one upload by its id: a repeated complete answers the same item.",
	"POST /api/v1/channels/{id}/held/replay":                           "Opens the held messages it names: an opened message is no longer held, so a repeat passes it by.",
	"POST /api/v1/channels/{id}/held/discard":                          "Discards the held messages it names: a repeat finds them gone.",
	// One value per channel or workspace, replaced: repeats leave one.
	"POST /api/v1/channels/{id}/email/secret":   "Replaces the channel's inbound secret: repeats leave one secret, the last.",
	"POST /api/v1/channels/email/secret":        "Replaces the default channel's inbound secret: repeats leave one secret, the last.",
	"POST /api/v1/results/share-link":           "One link per agent: a new link replaces the old one, so repeats leave one link.",
	"POST /api/v1/conversations/{id}/ai-drafts": "The person's earlier draft on the conversation is replaced: repeats leave one draft.",
	"POST /api/v1/conversations/{id}/ai-triage": "The open suggestion is replaced: repeats leave one suggestion.",
	"POST /api/v1/kb/images":                    "Stored by content: the same bytes again answer the image already stored.",
	"POST /api/v1/kb/media-uploads":             "An upload in progress holds no media; one left without parts expires, and the same bytes complete into the same item.",
	// Checks and reads sent as POST: they store a status at most.
	"POST /api/v1/channels/{id}/check":                     "Reads the channel's state again; it changes nothing.",
	"POST /api/v1/channels/{id}/email/domain-verification": "Checks the domain's DNS and marks it verified: a repeat checks again.",
	"POST /api/v1/channels/email/domain-verification":      "Checks the domain's DNS and marks it verified: a repeat checks again.",
	"POST /api/v1/channels/email/inbound/recipient":        "Answers whether an address takes mail; it stores nothing.",
	"POST /api/v1/ai/models":                               "Lists the models a key may use; nothing is saved.",
	"POST /api/v1/ai/providers/{id}/check":                 "Checks one provider's key and stores its last result.",
	"POST /api/v1/results/shared":                          "Reads the totals a share link shows; it stores nothing.",
	"POST /api/v1/help-center/theme/check":                 "Checks a candidate theme without saving it; it stores nothing.",
	"POST /api/v1/export/link":                             "Stores no row: a short-lived signed link held in memory, to the same export.",
	// Sessions opened at another service: nothing is stored here until it calls back.
	"POST /api/v1/channels/{id}/email/gmail":     "Answers Google's consent address; the connection is stored only when Google calls back, once.",
	"POST /api/v1/channels/{id}/email/microsoft": "Answers Microsoft's consent address; the connection is stored only when Microsoft calls back, once.",
	"POST /api/v1/channels/email/gmail":          "Answers Google's consent address; the connection is stored only when Google calls back, once.",
	"POST /api/v1/channels/email/microsoft":      "Answers Microsoft's consent address; the connection is stored only when Microsoft calls back, once.",
	"POST /api/v1/billing/checkout":              "Opens a checkout session at the payment provider; nothing is stored here until it is paid.",
	"POST /api/v1/billing/portal":                "Opens a portal session at the payment provider; nothing is stored here.",
	// Deliveries from other servers, which name each delivery themselves.
	"POST /api/v1/channels/email/inbound": "Mail from the relay: a Message-ID already stored is a duplicate, so a redelivery stores nothing.",
	"POST /api/v1/webhooks/polar":         "Polar's redelivery of an event is recognised and not applied twice.",
	"POST /api/v1/webhooks/resend":        "Resend's events fail one named reply: a redelivery finds it failed already.",
}

// maxPostWithoutKey is the ceiling of postWithoutKey; lower it when a route leaves the list.
const maxPostWithoutKey = 58

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

// A press repeated by a double click, a slow network or a retry must store one row, not two: every POST
// route names the submit with an Idempotency-Key, or says here why it need not.
func TestEveryPostRouteTakesAnIdempotencyKey(t *testing.T) {
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
		keyed := takesIdempotencyKey(path, path.Post)
		_, exempt := postWithoutKey[key]
		switch {
		case !keyed && !exempt:
			t.Errorf("%s (%s) takes no Idempotency-Key: add the CreateIdempotencyKey parameter, the 200 repeat and the 422 "+
				"idempotency_key_reused answer to the contract and run the handler through createOnce, or list it in "+
				"postWithoutKey with why a repeat stores nothing twice", key, path.Post.OperationID)
		case keyed && exempt:
			t.Errorf("postWithoutKey lists %s, which takes an Idempotency-Key: remove it and lower maxPostWithoutKey", key)
		}
	}
	for key, why := range postWithoutKey {
		if !seen[key] {
			t.Errorf("postWithoutKey lists %s, which is not a POST route: remove it", key)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("postWithoutKey lists %s without saying why a repeat stores nothing twice", key)
		}
	}
	if len(postWithoutKey) > maxPostWithoutKey {
		t.Errorf("postWithoutKey has %d routes, over the ceiling of %d: give the new POST route an Idempotency-Key instead",
			len(postWithoutKey), maxPostWithoutKey)
	}
}
