package mcpserver

import (
	"net/url"
	"strings"
)

// The filters of the article list page that colliedesk_kb_articles_search (and the CSV export) take
// besides query, status, collection_id, state, has_draft and visibility. They map to the query
// parameters of GET /api/v1/kb/articles.
const (
	kbDateBoundProperty = `{"type":"string","maxLength":40,"description":"An RFC 3339 time, or a date such as 2026-10-01 for midnight in the workspace's timezone. After keeps that instant, before leaves it out."}`

	kbListFilterProperties = `"include_subcollections":{"type":"boolean","description":"With collection_id: also the articles of its collections at every depth, and those listed in any of them."},` +
		`"author":{"type":"array","maxItems":50,"items":{"type":"string",` + uuid + `},"description":"Team member ids (colliedesk_agents_list); articles credited to one of them."},` +
		`"tags":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":40},"description":"Published tags (colliedesk_kb_tags_list), matched as they are saved: trimmed, lowercase."},` +
		`"tag_match":{"type":"string","enum":["any","all"],"description":"any (default): an article with one of tags; all: with every one."},` +
		`"created_after":` + kbDateBoundProperty + `,"created_before":` + kbDateBoundProperty + `,"updated_after":` + kbDateBoundProperty + `,"updated_before":` + kbDateBoundProperty + `,` +
		`"translation":{"type":"string","enum":["missing","outdated"],"description":"missing: no translation in locale, or without locale in some help center language; outdated: one marked outdated."},` +
		`"locale":{"type":"string","maxLength":35,"description":"With translation: the help center language (colliedesk_help_center_languages_get)."}`

	kbLanguagesOutput = `"languages":{"type":"array","description":"Its translations outside the trash; a help center language not here is missing.",` +
		`"items":{"type":"object","required":["locale","outdated"],"properties":{"locale":{"type":"string"},"outdated":{"type":"boolean"}}}}`
)

// kbListFilterArguments are the arguments of kbListFilterProperties.
type kbListFilterArguments struct {
	IncludeSubcollections bool     `json:"include_subcollections"`
	Author                []string `json:"author"`
	Tags                  []string `json:"tags"`
	TagMatch              string   `json:"tag_match"`
	CreatedAfter          string   `json:"created_after"`
	CreatedBefore         string   `json:"created_before"`
	UpdatedAfter          string   `json:"updated_after"`
	UpdatedBefore         string   `json:"updated_before"`
	Translation           string   `json:"translation"`
	Locale                string   `json:"locale"`
}

// values writes the arguments as the query parameters of GET /api/v1/kb/articles.
func (args kbListFilterArguments) values() url.Values {
	query := url.Values{}
	if args.IncludeSubcollections {
		query.Set("include_subcollections", "true")
	}
	if len(args.Author) > 0 {
		query.Set("author", strings.Join(args.Author, ","))
	}
	for _, tag := range args.Tags {
		query.Add("tag", tag)
	}
	for key, value := range map[string]string{"tag_match": args.TagMatch, "created_after": args.CreatedAfter, "created_before": args.CreatedBefore,
		"updated_after": args.UpdatedAfter, "updated_before": args.UpdatedBefore, "translation": args.Translation, "locale": args.Locale} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return query
}
