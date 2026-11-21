package mcpserver

import (
	"net/http"
	"net/url"
	"strings"
)

// Bulk actions on knowledge base articles: one action on up to 500 articles named by ids or by the
// filters of colliedesk_kb_articles_search, each article answered on its own, the whole batch undone
// in one call within ten minutes.

const (
	kbBulkActions        = `["move","visibility","author","tags_add","tags_remove","send_for_review","approve","publish","unpublish","trash"]`
	kbBulkTeamTextSchema = `{"type":["object","null"],"required":["title"],"description":"null for an id this workspace has no article for.","properties":{"title":{"type":"string"}}}`
	kbBulkItemProperties = `"id":{"type":"string"},"number":{"type":["integer","null"]},"team_text":` + kbBulkTeamTextSchema + `,` +
		`"error":{"oneOf":[{"type":"null"},` + kbBulkErrorSchema + `],"description":"Why it was refused or not undone: the same error the call for that article alone gives."}`
)

func kbArticlesBulkSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_articles_bulk",
		Title: "Change many knowledge base articles at once",
		Description: `Apply one action to up to 500 articles: ids, or filter (the filters of colliedesk_kb_articles_search; {} for every article), never both. ` +
			`Actions: move (collection_id), visibility, author (author_agent_id), tags_add and tags_remove (tags), send_for_review (reviewer_ids, note), approve (note), publish, unpublish, trash. ` +
			`approve, publish and unpublish need kb_articles:publish, trash needs kb_articles:delete, and an article that would leave or join the help center needs kb_articles:publish too. ` +
			`Each article is answered on its own: done, unchanged (it already was so) or refused with the error the call for it alone gives, such as approval_required. ` +
			`Undo the whole batch with colliedesk_kb_articles_bulk_undo and operation.id before operation.undo_until (ten minutes).` + "\n\n" + kbTextNotice,
		REST: []string{"POST /api/v1/kb/articles/bulk"},
		Input: `{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string","enum":` + kbBulkActions + `},` +
			`"ids":{"type":"array","minItems":1,"maxItems":500,"uniqueItems":true,"items":{"type":"string",` + uuid + `}},` +
			`"filter":{"type":"object","additionalProperties":false,"properties":{` + kbArticleListInput + `}},` +
			`"collection_id":{"type":"string",` + uuid + `,"description":"move: the collection to move them to (colliedesk_kb_collections_list)."},` +
			`"visibility":{` + kbVisibilityEnum + `},"author_agent_id":{"type":"string",` + uuid + `,"description":"author: the team member to credit (colliedesk_agents_list)."},` +
			`"tags":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","minLength":1,"maxLength":40}},` +
			`"reviewer_ids":{"type":"array","minItems":1,"maxItems":10,"items":{"type":"string",` + uuid + `},"description":"send_for_review: teammates who may publish."},` + kbReviewNoteInput + `}}`,
		Output: `{"type":"object","required":["operation","counts","results"],"properties":{` +
			`"operation":{"type":["object","null"],"required":["id","action","undo_until"],"description":"null when nothing undo can put back changed.",` +
			`"properties":{"id":{"type":"string"},"action":{"type":"string"},"undo_until":{"type":"string"}}},` +
			`"counts":{"type":"object","required":["done","unchanged","refused"],"properties":{"done":{"type":"integer"},"unchanged":{"type":"integer"},"refused":{"type":"integer"}}},` +
			`"results":{"type":"array","items":{"type":"object","required":["id","number","team_text","outcome","undoable","error"],"properties":{` + kbBulkItemProperties + `,` +
			`"outcome":{"type":"string","enum":["done","unchanged","refused"]},"undoable":{"type":"boolean","description":"false for a change undo cannot put back, such as an approval."}}}}}}`,
		run: kbArticlesBulk,
	}
}

func kbArticlesBulkUndoSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_articles_bulk_undo",
		Title: "Undo a bulk change of knowledge base articles",
		Description: `Put back what one colliedesk_kb_articles_bulk call changed, once and within ten minutes. ` +
			`An article changed again since is left as it is and answered not_undone, so undo never overwrites newer work; ` +
			`putting a published article back on the help center needs kb_articles:publish, as by hand.` + "\n\n" + kbTextNotice,
		REST:  []string{"POST /api/v1/kb/bulk-operations/{id}/undo"},
		Input: `{"type":"object","additionalProperties":false,"required":["operation_id"],"properties":{"operation_id":{"type":"string",` + uuid + `,"description":"operation.id of the bulk answer."}}}`,
		Output: `{"type":"object","required":["operation","counts","results"],"properties":{` +
			`"operation":{"type":"object","required":["id","action","undone_at"],"properties":{"id":{"type":"string"},"action":{"type":"string"},"undone_at":{"type":"string"}}},` +
			`"counts":{"type":"object","required":["undone","not_undone"],"properties":{"undone":{"type":"integer"},"not_undone":{"type":"integer"}}},` +
			`"results":{"type":"array","items":{"type":"object","required":["id","number","team_text","outcome","error"],"properties":{` + kbBulkItemProperties + `,` +
			`"outcome":{"type":"string","enum":["undone","not_undone"]}}}}}}`,
		run: kbArticlesBulkUndo,
	}
}

// kbBulkRow is one article of a bulk answer as the REST API gives it.
type kbBulkRow struct {
	ID       string       `json:"id"`
	Number   *int64       `json:"number"`
	Title    string       `json:"title"`
	Outcome  string       `json:"outcome"`
	Undoable *bool        `json:"undoable"`
	Error    *kbBulkError `json:"error"`
}

// out puts the title under team_text; an id without an article has neither number nor title.
func (row kbBulkRow) out() map[string]any {
	out := map[string]any{"id": row.ID, "number": row.Number, "team_text": nil, "outcome": row.Outcome, "error": row.Error}
	if row.Number != nil {
		out["team_text"] = kbTitle{row.Title}
	}
	if row.Undoable != nil {
		out["undoable"] = *row.Undoable
	}
	return out
}

type kbBulkAnswer struct {
	Operation map[string]any `json:"operation"`
	Counts    map[string]int `json:"counts"`
	Results   []kbBulkRow    `json:"results"`
}

func (answer kbBulkAnswer) out() map[string]any {
	results := make([]map[string]any, 0, len(answer.Results))
	for _, row := range answer.Results {
		results = append(results, row.out())
	}
	return map[string]any{"operation": answer.Operation, "counts": answer.Counts, "results": results}
}

func kbArticlesBulk(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Filter *kbArticleListArguments `json:"filter"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	for key, value := range arguments {
		body[key] = value
	}
	if args.Filter != nil {
		body["filter"] = kbBulkFilter(args.Filter.listValues())
	}
	var answer kbBulkAnswer
	if err := c.send(http.MethodPost, "/api/v1/kb/articles/bulk", body, &answer); err != nil {
		return nil, err
	}
	return answer.out(), nil
}

// kbBulkFilter is the list query as the filter object the bulk route takes: author and tag as lists,
// the yes-or-no filters as booleans.
func kbBulkFilter(query url.Values) map[string]any {
	filter := map[string]any{}
	for key, values := range query {
		switch key {
		case "author":
			filter[key] = strings.Split(values[0], ",")
		case "tag":
			filter[key] = values
		case "include_subcollections", "has_draft":
			filter[key] = values[0] == "true"
		default:
			filter[key] = values[0]
		}
	}
	return filter
}

func kbArticlesBulkUndo(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"operation_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var answer kbBulkAnswer
	if err := c.send(http.MethodPost, "/api/v1/kb/bulk-operations/"+url.PathEscape(args.ID)+"/undo", map[string]any{}, &answer); err != nil {
		return nil, err
	}
	return answer.out(), nil
}
