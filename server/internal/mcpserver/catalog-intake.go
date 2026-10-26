package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// The intake queue of chat channels: what a customer said in one go on a scope that collects messages
// (a card), decided by the channel's rules, AI or a person. What customers wrote, and their names, come
// back under untrusted_customer_content; so do a decision's subject and AI's reason, written from it, the
// name of the chat place and the subject and customer of a conversation a card may join.

const (
	intakeCardSchema = `{"type":"object","required":["id","channel_id","state","rules","item_count","untrusted_customer_content"],"properties":{` +
		`"id":{"type":"string"},"channel_id":{"type":"string"},"scope":{"type":"string"},"thread_id":{"type":["string","null"]},"lane_key":{"type":"string"},` +
		`"contact_id":{"type":["string","null"]},"state":{"type":"string","enum":["deciding","review","created","attached","ignored","applied","cancelled"]},` +
		`"review_reason":{"type":["string","null"],"description":"Why it waits for a person: no_command, ai_unavailable, lane_blocked (behind an earlier card of the same sender), requeued…"},` +
		`"rules":{"type":"object","description":"What the channel's rules decided: what applies when AI does not decide in time."},` +
		`"conversation_id":{"type":["string","null"]},"decided_by":{"type":["string","null"],"enum":["rules","ai","agent",null]},"decided_at":{"type":["string","null"]},` +
		`"decided_by_agent_id":{"type":["string","null"]},"ignore_reason":{"type":["string","null"],"enum":["chat","handled_in_chat","duplicate","other",null]},` +
		`"fallback":{"type":"boolean"},"opened_at":{"type":"string"},"closed_at":{"type":"string"},"decide_by":{"type":["string","null"]},"item_count":{"type":"integer"},` +
		`"place_kind":{"type":["string","null"],"enum":["direct","group",null],"description":"A one-to-one chat or a group."},` +
		`"untrusted_customer_content":{"type":"object","properties":{"sender_name":{"type":"string"},"sender":{"type":"string"},"preview":{"type":"string"},` +
		`"place_name":{"type":"string","description":"The group's name on the channel."}}}}}`
	intakeCandidateSchema = `{"type":"object","required":["conversation_id","status","resolved","untrusted_customer_content"],"properties":{"conversation_id":{"type":"string"},` +
		`"status":{"type":"string"},"resolved":{"type":"boolean","description":"Joining it reopens it."},"contact_id":{"type":"string"},"channel_id":{"type":["string","null"]},` +
		`"updated_at":{"type":"string"},"untrusted_customer_content":{"type":"object","properties":{"subject":{"type":"string"},"contact_name":{"type":"string"}}}}}`
	intakeDetailSchema = `{"type":"object","required":["card","items","decisions","candidates","commands"],"properties":{"card":` + intakeCardSchema + `,` +
		`"items":{"type":"array","items":` + chatItemSchema + `},"decisions":{"type":"array","items":{"type":"object"}},` +
		`"candidates":{"type":"array","items":` + intakeCandidateSchema + `,"description":"Where the card may be added without the reply going to someone else, the rules' target first; empty once decided."},` +
		`"commands":{"type":"array","items":{"type":"string"},"description":"The channel's commands that open a request, for create."}}}`
	intakeIDInput  = `"gathering_id":{"type":"string",` + uuid + `,"description":"From colliedesk_intake_list."}`
	intakeNotFound = "No intake card with that id in this workspace. List them with colliedesk_intake_list."
)

func intakeListSpec() Spec {
	return Spec{
		Name: "colliedesk_intake_list", Title: "List the intake queue",
		Description: `List the cards of the intake queue of chat channels: what a customer said in one go on a scope that collects messages. ` +
			`state review (default): what waits for a person, oldest first; deciding: what waits for AI; ignored: what was set aside; decided: what was opened, joined or set aside, newest first. ` +
			`channel_id and scope narrow it; decided_by (rules, ai, agent) keeps who decided (ignored with ai: what AI set aside). Decide a card with colliedesk_intake_decide. Email and web forms have no cards. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/intake/gatherings"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"state":{"type":"string","enum":["review","deciding","ignored","decided"]},` +
			`"channel_id":{"type":"string",` + uuid + `},"scope":{"type":"string","pattern":"^[a-z_]{1,40}$"},"decided_by":{"type":"string","enum":["rules","ai","agent"]},` +
			`"cursor":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":50,"default":50}}}`,
		Output: `{"type":"object","required":["items","next_cursor"],"properties":{"items":{"type":"array","items":` + intakeCardSchema + `},"next_cursor":{"type":["string","null"]}}}`,
		run:    intakeList,
	}
}

func intakeGetSpec() Spec {
	return Spec{
		Name: "colliedesk_intake_get", Title: "Read an intake card",
		Description: `Read a card of the intake queue: everything its customer said, oldest first, every decision made on it (by AI or a person), newest first, ` +
				`and while it is to be decided the conversations it may join (candidates) and the channel's commands that open a request. ` +
			untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/intake/gatherings/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["gathering_id"],"properties":{` + intakeIDInput + `}}`,
		Output: intakeDetailSchema,
		run:    intakeGet,
	}
}

func intakeDecideSpec() Spec {
	return Spec{
		Name: "colliedesk_intake_decide", Title: "Decide an intake card",
		Description: `Decide a card that waits for a person or for AI: create opens a conversation with what the customer said, as one message ` +
			`(optional command: one of the channel's commands that opens a request, for its tags and priority; optional subject); attach adds it to conversation_id ` +
			`(one of the card's candidates, so the reply goes to the same customer); ignore sets it aside, with an optional ignore_reason (colliedesk_intake_requeue takes it back). The same decision again changes nothing; another one once the card is decided ` +
			`fails with gathering_decided and how it stands. Opening a conversation may tell the customer it was received, so create needs the send tier; ` +
			`a token below it files a request a person approves.` + "\n\nTier: draft (create: send).",
		Tier: "draft",
		REST: []string{"POST /api/v1/intake/gatherings/{id}/decide"},
		Input: `{"type":"object","additionalProperties":false,"required":["gathering_id","action"],"properties":{` + intakeIDInput + `,` +
			`"action":{"type":"string","enum":["create","attach","ignore"]},"conversation_id":{"type":"string",` + uuid + `,"description":"With attach."},` +
			`"command":{"type":"string","minLength":1,"maxLength":40},"subject":{"type":"string","minLength":1,"maxLength":200},` +
				`"ignore_reason":{"type":"string","enum":["chat","handled_in_chat","duplicate","other"],"description":"With ignore: not a request, answered in the chat, said already, or another reason."}}}`,
		Output: `{"type":"object","properties":{"card":` + intakeCardSchema + `,"items":{"type":"array"},"decisions":{"type":"array"},"agent_request":` + filedRequestSchema +
			`,"next":{"type":"string"}},"anyOf":[{"required":["card"]},{"required":["agent_request","next"]}]}`,
		run: intakeDecide,
	}
}

func intakeRequeueSpec() Spec {
	return Spec{
		Name: "colliedesk_intake_requeue", Title: "Put an intake card back",
		Description: `Take a card that was set aside (by a person, AI or the rules) back to the queue; its decisions stay in its history. ` +
			`A card in the queue already is left as it is; one opened or joined fails with gathering_decided.` + "\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"POST /api/v1/intake/gatherings/{id}/requeue"},
		Input:  `{"type":"object","additionalProperties":false,"required":["gathering_id"],"properties":{` + intakeIDInput + `}}`,
		Output: intakeDetailSchema,
		run:    intakeRequeue,
	}
}

func intakeSummarySpec() Spec {
	return Spec{
		Name: "colliedesk_intake_summary", Title: "Summarize the intake queue",
		Description: `Say at a glance what the intake queue holds: whether the workspace has a chat channel with intake (available), how many cards wait ` +
			`for a person (waiting) and for AI (deciding), and every channel and scope a card can come from. Counts and names only.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/intake/summary"},
		Input: `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: `{"type":"object","required":["available","waiting","deciding","scopes"],"properties":{"available":{"type":"boolean"},"waiting":{"type":"integer"},` +
			`"deciding":{"type":"integer"},"scopes":{"type":"array","items":{"type":"object","properties":{"channel_id":{"type":"string"},"channel_name":{"type":"string"},"scope":{"type":"string"}}}}}}`,
		run: intakeSummary,
	}
}

func intakeStatsSpec() Spec {
	return Spec{
		Name: "colliedesk_intake_stats", Title: "Count the intake",
		Description: `Count the cards that closed in the last days (one channel, or all): by state, by who decided (rules, AI, a person), ` +
			`how many fell back on the rules, and how many of AI's decisions a person changed.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/intake/stats"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"channel_id":{"type":"string",` + uuid + `},` +
			`"days":{"type":"integer","minimum":1,"maximum":90,"default":7}}}`,
		Output: `{"type":"object","required":["stats"],"properties":{"stats":{"type":"object"}}}`,
		run:    intakeStats,
	}
}

func conversationIntakeGetSpec() Spec {
	return Spec{
		Name: "colliedesk_conversation_intake_get", Title: "Why messages are in a conversation",
		Description: `Read why each message from a channel is in a conversation: what decided it (the rules by match or by default, a person, or AI, ` +
			`from decision_basis and decision_rule) and the chat cards it came in, with their decisions. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversations/{id}/intake"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{"conversation_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["messages","gatherings"],"properties":{"messages":{"type":"array","items":{"type":"object"}},"gatherings":{"type":"array","items":` + intakeDetailSchema + `}}}`,
		run:    conversationIntakeGet,
	}
}

type intakeCardJSON struct {
	ID             string          `json:"id"`
	ChannelID      string          `json:"channel_id"`
	Scope          string          `json:"scope"`
	ThreadID       *string         `json:"thread_id"`
	LaneKey        string          `json:"lane_key"`
	ContactID      *string         `json:"contact_id"`
	State          string          `json:"state"`
	ReviewReason   *string         `json:"review_reason"`
	Rules          json.RawMessage `json:"rules"`
	ConversationID *string         `json:"conversation_id"`
	DecidedBy      *string         `json:"decided_by"`
	DecidedAt      *string         `json:"decided_at"`
	DecidedByAgent *string         `json:"decided_by_agent_id"`
	IgnoreReason   *string         `json:"ignore_reason"`
	Fallback       bool            `json:"fallback"`
	OpenedAt       string          `json:"opened_at"`
	ClosedAt       string          `json:"closed_at"`
	DecideBy       *string         `json:"decide_by"`
	ItemCount      int             `json:"item_count"`
	Sender         struct {
		Value *string `json:"value"`
		Name  *string `json:"name"`
	} `json:"sender"`
	Place *struct {
		Kind string  `json:"kind"`
		Name *string `json:"name"`
	} `json:"place"`
	Preview string `json:"preview"`
}

type intakeCandidateJSON struct {
	ConversationID string  `json:"conversation_id"`
	Subject        string  `json:"subject"`
	Status         string  `json:"status"`
	ContactID      string  `json:"contact_id"`
	ContactName    string  `json:"contact_name"`
	ChannelID      *string `json:"channel_id"`
	UpdatedAt      string  `json:"updated_at"`
	Resolved       bool    `json:"resolved"`
}

type intakeDetailJSON struct {
	intakeCardJSON
	Items      []chatItemJSON        `json:"items"`
	Decisions  []map[string]any      `json:"decisions"`
	Candidates []intakeCandidateJSON `json:"candidates"`
	Commands   []string              `json:"commands"`
}

func intakeCardOut(card intakeCardJSON) map[string]any {
	untrusted := map[string]any{"preview": card.Preview}
	if card.Sender.Name != nil {
		untrusted["sender_name"] = *card.Sender.Name
	}
	if card.Sender.Value != nil {
		untrusted["sender"] = *card.Sender.Value
	}
	var placeKind *string
	if card.Place != nil {
		placeKind = &card.Place.Kind
		if card.Place.Name != nil {
			untrusted["place_name"] = *card.Place.Name
		}
	}
	return map[string]any{"id": card.ID, "channel_id": card.ChannelID, "scope": card.Scope, "thread_id": card.ThreadID, "lane_key": card.LaneKey,
		"contact_id": card.ContactID, "state": card.State, "review_reason": card.ReviewReason, "rules": card.Rules, "conversation_id": card.ConversationID,
		"decided_by": card.DecidedBy, "decided_at": card.DecidedAt, "decided_by_agent_id": card.DecidedByAgent, "ignore_reason": card.IgnoreReason,
		"fallback": card.Fallback, "opened_at": card.OpenedAt, "closed_at": card.ClosedAt, "decide_by": card.DecideBy, "item_count": card.ItemCount,
		"place_kind": placeKind, "untrusted_customer_content": untrusted}
}

// intakeDetailOut keeps a decision's subject and reason, written from what the customer said, apart.
func intakeDetailOut(detail intakeDetailJSON) map[string]any {
	items := make([]map[string]any, 0, len(detail.Items))
	for _, item := range detail.Items {
		items = append(items, chatItemOut(item))
	}
	decisions := make([]map[string]any, 0, len(detail.Decisions))
	for _, decision := range detail.Decisions {
		untrusted := map[string]any{}
		for _, key := range []string{"subject", "reason"} {
			if value, ok := decision[key]; ok && value != nil {
				untrusted[key] = value
			}
			delete(decision, key)
		}
		decision["untrusted_customer_content"] = untrusted
		decisions = append(decisions, decision)
	}
	candidates := make([]map[string]any, 0, len(detail.Candidates))
	for _, c := range detail.Candidates {
		candidates = append(candidates, map[string]any{"conversation_id": c.ConversationID, "status": c.Status, "resolved": c.Resolved, "contact_id": c.ContactID,
			"channel_id": c.ChannelID, "updated_at": c.UpdatedAt, "untrusted_customer_content": map[string]any{"subject": c.Subject, "contact_name": c.ContactName}})
	}
	commands := detail.Commands
	if commands == nil {
		commands = []string{}
	}
	return map[string]any{"card": intakeCardOut(detail.intakeCardJSON), "items": items, "decisions": decisions, "candidates": candidates, "commands": commands}
}

func intakePath(id, rest string) string {
	return "/api/v1/intake/gatherings/" + url.PathEscape(id) + rest
}

func intakeList(c *call, arguments map[string]any) (any, error) {
	var page struct {
		Items []intakeCardJSON `json:"items"`
		Next  *string          `json:"next_cursor"`
	}
	if err := c.get("/api/v1/intake/gatherings", pagingQuery(arguments, "state", "channel_id", "scope", "decided_by"), &page); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, card := range page.Items {
		items = append(items, intakeCardOut(card))
	}
	return map[string]any{"items": items, "next_cursor": page.Next}, nil
}

func intakeGet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["gathering_id"].(string)
	c.notFound = intakeNotFound
	var detail intakeDetailJSON
	if err := c.get(intakePath(id, ""), nil, &detail); err != nil {
		return nil, err
	}
	return intakeDetailOut(detail), nil
}

func intakeDecide(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["gathering_id"].(string)
	body := map[string]any{}
	for key, value := range arguments {
		if key != "gathering_id" {
			body[key] = value
		}
	}
	c.notFound = intakeNotFound
	var detail intakeDetailJSON
	if err := c.send(http.MethodPost, intakePath(id, "/decide"), body, &detail); err != nil {
		return nil, err
	}
	return intakeDetailOut(detail), nil
}

func intakeRequeue(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["gathering_id"].(string)
	c.notFound = intakeNotFound
	var detail intakeDetailJSON
	if err := c.send(http.MethodPost, intakePath(id, "/requeue"), nil, &detail); err != nil {
		return nil, err
	}
	return intakeDetailOut(detail), nil
}

func intakeSummary(c *call, _ map[string]any) (any, error) {
	var summary json.RawMessage
	if err := c.get("/api/v1/intake/summary", nil, &summary); err != nil {
		return nil, err
	}
	return summary, nil
}

func intakeStats(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	if id, _ := arguments["channel_id"].(string); id != "" {
		query.Set("channel_id", id)
	}
	if days, ok := arguments["days"].(float64); ok {
		query.Set("days", strconv.Itoa(int(days)))
	}
	var stats json.RawMessage
	if err := c.get("/api/v1/intake/stats", query, &stats); err != nil {
		return nil, err
	}
	return map[string]any{"stats": stats}, nil
}

func conversationIntakeGet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["conversation_id"].(string)
	var found struct {
		Messages   []json.RawMessage  `json:"messages"`
		Gatherings []intakeDetailJSON `json:"gatherings"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(id)+"/intake", nil, &found); err != nil {
		return nil, err
	}
	gatherings := make([]map[string]any, 0, len(found.Gatherings))
	for _, detail := range found.Gatherings {
		gatherings = append(gatherings, intakeDetailOut(detail))
	}
	return map[string]any{"messages": found.Messages, "gatherings": gatherings}, nil
}
