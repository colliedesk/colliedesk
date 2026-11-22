package mcpserver

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const kbGapKinds = `["search_no_result","vote_no","chat_handoff","chat_unanswered","recurring_topic"]`

// kbGapOutput is one knowledge gap. Its title is the words of a reader, so it sits under
// untrusted_customer_content.
const kbGapOutput = `{"type":"object","required":[` + kbGapRequired + `],"properties":{` + kbGapProperties + `}}`

const kbGapRequired = `"id","status","kinds","askers","recent_askers","first_seen","last_seen","fixed_by_article","fixed_automatically","status_changed_by","status_changed_at","untrusted_customer_content"`

const kbGapProperties = `"id":{"type":"string"},"status":{"type":"string","enum":["open","fixed","dismissed"]},` +
	`"kinds":{"type":"array","description":"The kinds of its signals.","items":{"type":"string","enum":` + kbGapKinds + `}},` +
	`"askers":{"type":"integer","description":"Distinct askers since the first signal."},` +
	`"recent_askers":{"type":"integer","description":"Distinct askers in the last 30 days: the rank."},` +
	`"first_seen":{"type":"string"},"last_seen":{"type":"string"},` +
	`"fixed_by_article":{"type":["object","null"],"description":"The article that answers it; null unless it is fixed by one.","properties":{"id":{"type":"string"},"title":{"type":"string"}}},` +
	`"fixed_automatically":{"type":"boolean","description":"True when a matching article was published and no person changed the gap since."},` +
	`"status_changed_by":{"type":["object","null"],"description":"Who changed the status last; null when no person did.","properties":{"agent_id":{"type":["string","null"]},"name":{"type":"string"}}},` +
	`"status_changed_at":{"type":["string","null"]},` +
	`"untrusted_customer_content":{"type":"object","required":["title"],"properties":{"title":{"type":"string",` +
	`"description":"The words of the signal that opened it, masked: [email removed] and [number removed] replace an email address and a run of 6 or more digits."}}}`

const kbGapIDProperty = `"gap_id":{"type":"string",` + uuid + `,"description":"From colliedesk_kb_gaps_list."}`

func kbGapsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_gaps_list",
		Title: "List what readers asked for and did not find",
		Description: `List the knowledge gaps: what readers asked for and found no answer to, grouped by their words, most distinct askers in 30 days first. ` +
			`A gap gathers help center searches with no result, "No" votes with their "What was missing?" line, help chats handed to a person or not answered from an article, ` +
			`and resolved conversations no article title matches. Open gaps unless status says otherwise; kind keeps the gaps with a signal of that kind. ` +
			`Read one's signals with colliedesk_kb_gap_get; write the missing article with colliedesk_kb_article_create, then link it with colliedesk_kb_gap_update.` + "\n\n" + untrustedNotice,
		REST: []string{"GET /api/v1/kb/gaps"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"status":{"type":"string","enum":["open","fixed","dismissed"],"description":"Default open."},` +
			`"kind":{"type":"string","enum":` + kbGapKinds + `},"limit":{"type":"integer","minimum":1,"maximum":200,"description":"Default 50."}}}`,
		Output: `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":` + kbGapOutput + `}}}`,
		run:    kbGapsList,
	}
}

func kbGapGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_gap_get",
		Title: "Read what readers asked for in one knowledge gap",
		Description: `Read one knowledge gap with its newest 100 signals, newest first: what each reader searched, wrote or asked, masked. ` +
			`Nothing tells the askers apart. Link the gap to the article that answers it, or dismiss it, with colliedesk_kb_gap_update.` + "\n\n" + untrustedNotice,
		REST:  []string{"GET /api/v1/kb/gaps/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["gap_id"],"properties":{` + kbGapIDProperty + `}}`,
		Output: `{"type":"object","required":[` + kbGapRequired + `,"signal_count","signals"],"properties":{` + kbGapProperties + `,` +
			`"signal_count":{"type":"integer","description":"Every signal of the gap; signals lists the newest 100."},` +
			`"signals":{"type":"array","items":{"type":"object","required":["kind","created_at","untrusted_customer_content"],"properties":{` +
			`"kind":{"type":"string","enum":` + kbGapKinds + `},"created_at":{"type":"string"},` +
			`"untrusted_customer_content":{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}}}}}}`,
		run: kbGapGet,
	}
}

func kbGapUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_gap_update",
		Title: "Link a knowledge gap to its article, or dismiss it",
		Description: `Mark a knowledge gap fixed by the article that answers it (fixed_by_article_id, status may be left out), dismiss it (status dismissed: it leaves the open list and keeps counting) ` +
			`or open it again (status open, which also undoes a fix made when a matching article was published). Opening or dismissing clears the article. ` +
			`A fixed gap opens again by itself when it is asked for more than 7 days after the fix.` + "\n\n" + untrustedNotice,
		REST: []string{"PATCH /api/v1/kb/gaps/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["gap_id"],"properties":{` + kbGapIDProperty + `,` +
			`"status":{"type":"string","enum":["open","fixed","dismissed"]},` +
			`"fixed_by_article_id":{"type":"string",` + uuid + `,"description":"An article of the workspace outside the trash; find it with colliedesk_kb_articles_search."}}}`,
		Output: kbGapOutput,
		run:    kbGapUpdate,
	}
}

func kbGapsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Status string `json:"status"`
		Kind   string `json:"kind"`
		Limit  int    `json:"limit"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for name, value := range map[string]string{"status": args.Status, "kind": args.Kind} {
		if value != "" {
			query.Set(name, value)
		}
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	var page struct {
		Items []domain.KBGap `json:"items"`
	}
	if err := c.get("/api/v1/kb/gaps", query, &page); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, gap := range page.Items {
		items = append(items, kbGapAnswer(gap))
	}
	return map[string]any{"items": items}, nil
}

func kbGapGet(c *call, arguments map[string]any) (any, error) {
	id, err := kbGapID(c, arguments)
	if err != nil {
		return nil, err
	}
	var detail domain.KBGapDetail
	if err := c.get("/api/v1/kb/gaps/"+url.PathEscape(id), nil, &detail); err != nil {
		return nil, err
	}
	signals := make([]map[string]any, 0, len(detail.Signals))
	for _, signal := range detail.Signals {
		signals = append(signals, map[string]any{"kind": signal.Kind, "created_at": signal.CreatedAt,
			"untrusted_customer_content": map[string]any{"text": signal.Text}})
	}
	answer := kbGapAnswer(detail.KBGap)
	answer["signal_count"], answer["signals"] = detail.SignalCount, signals
	return answer, nil
}

func kbGapUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID      string `json:"gap_id"`
		Status  string `json:"status"`
		Article string `json:"fixed_by_article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if args.Status != "" {
		body["status"] = args.Status
	}
	if args.Article != "" {
		body["fixed_by_article_id"] = args.Article
	}
	if len(body) == 0 {
		return nil, &toolError{"Give status (open, fixed or dismissed), fixed_by_article_id or both."}
	}
	c.notFound = "No knowledge gap " + args.ID + " in this workspace, or fixed_by_article_id is not an article here outside the trash. " +
		"List the gaps with colliedesk_kb_gaps_list and find the article with colliedesk_kb_articles_search."
	var gap domain.KBGap
	if err := c.send(http.MethodPatch, "/api/v1/kb/gaps/"+url.PathEscape(args.ID), body, &gap); err != nil {
		return nil, err
	}
	return kbGapAnswer(gap), nil
}

func kbGapID(c *call, arguments map[string]any) (string, error) {
	args, err := decodeArguments[struct {
		ID string `json:"gap_id"`
	}](arguments)
	c.notFound = "No knowledge gap " + args.ID + " in this workspace. List them with colliedesk_kb_gaps_list."
	return args.ID, err
}

func kbGapAnswer(gap domain.KBGap) map[string]any {
	kinds := gap.Kinds
	if kinds == nil {
		kinds = []string{}
	}
	return map[string]any{
		"id": gap.ID, "status": gap.Status, "kinds": kinds, "askers": gap.Askers, "recent_askers": gap.RecentAskers,
		"first_seen": gap.FirstSeen, "last_seen": gap.LastSeen, "fixed_by_article": gap.FixedByArticle,
		"fixed_automatically": gap.FixedAutomatically, "status_changed_by": gap.StatusChangedBy, "status_changed_at": gap.StatusChangedAt,
		"untrusted_customer_content": map[string]any{"title": gap.Title},
	}
}
