package httpapi

import (
	"net/url"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbArticleFilter reads the filters of an article list (GET /api/v1/kb/articles and its CSV export)
// from query. A bare date is midnight in timezone, the workspace's. Limit and Cursor are the caller's.
// An error is a filterError that says how to write the value.
func kbArticleFilter(query url.Values, timezone string) (store.KBArticleFilter, error) {
	filter := store.KBArticleFilter{Query: query.Get("q"), Status: query.Get("status"), CollectionID: query.Get("collection_id"), Sort: query.Get("sort"),
		ReviewState: query.Get("state"), Visibility: query.Get("visibility"), Translation: query.Get("translation"), Locale: query.Get("locale")}
	if filter.Visibility != "" && domain.CheckKBVisibility(filter.Visibility) != nil {
		return filter, filterError("visibility is everyone, help_chat or agents: who can see the article now.")
	}
	if filter.ReviewState != "" && filter.ReviewState != store.KBInReview && filter.ReviewState != store.KBApproved {
		return filter, filterError("state is in_review or approved.")
	}
	var err error
	if filter.HasDraft, err = kbBoolParam(query, "has_draft"); err != nil {
		return filter, err
	}
	if filter.IncludeSubcollections, err = kbBoolParam(query, "include_subcollections"); err != nil {
		return filter, err
	}
	if filter.IncludeSubcollections && filter.CollectionID == "" {
		return filter, filterError("include_subcollections needs collection_id: the collection whose collections to include.")
	}
	if filter.Sort != "" && filter.Sort != store.KBSortRecent && filter.Sort != store.KBSortManual {
		return filter, filterError("sort is recent or manual.")
	}
	if len([]rune(filter.Query)) > maxFilterTextRunes {
		return filter, filterError("q holds at most 200 characters.")
	}
	if filter.Status != "" && domain.CheckKBStatus(filter.Status) != nil {
		return filter, filterError("status is draft or published.")
	}
	if raw := query.Get("author"); raw != "" {
		if filter.AuthorIDs, err = commaList("author", raw, isUUID); err != nil {
			return filter, filterError("author takes team member ids separated by commas, at most 50; GET /api/v1/agents lists them.")
		}
	}
	if tags := query["tag"]; len(tags) > 0 {
		if filter.Tags, err = domain.CleanKBTags(tags); err != nil {
			return filter, filterError("tag: " + err.Error())
		}
	}
	switch query.Get("tag_match") {
	case "", "any":
	case "all":
		filter.TagsAll = true
	default:
		return filter, filterError("tag_match is any (an article with one of the tags) or all (with every one).")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.UTC
	}
	for _, bound := range []struct {
		key string
		at  **time.Time
	}{{"created_after", &filter.CreatedAfter}, {"created_before", &filter.CreatedBefore}, {"updated_after", &filter.UpdatedAfter}, {"updated_before", &filter.UpdatedBefore}} {
		raw := query.Get(bound.key)
		if raw == "" {
			continue
		}
		at, err := kbDateBound(raw, location)
		if err != nil {
			return filter, filterError(bound.key + " is an RFC 3339 time with an offset, for example 2026-10-01T00:00:00Z, or a date such as 2026-10-01 (midnight in the workspace's timezone).")
		}
		*bound.at = &at
	}
	switch filter.Translation {
	case "", domain.KBLanguageMissing, domain.KBLanguageOutdated:
	default:
		return filter, filterError("translation is missing (no translation in locale, or in some language of the help center) or outdated.")
	}
	if filter.Locale != "" {
		if filter.Translation == "" {
			return filter, filterError("locale needs translation: missing or outdated in that language.")
		}
		if domain.CheckLocale(filter.Locale) != nil {
			return filter, filterError("locale is a language code of the help center, such as fr or pt-BR; GET /api/v1/help-center/languages lists them.")
		}
	}
	return filter, nil
}

func kbBoolParam(query url.Values, key string) (bool, error) {
	switch query.Get(key) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, filterError(key + " is true or false.")
}

// kbDateBound reads an RFC 3339 time, or a date as its midnight in location.
func kbDateBound(raw string, location *time.Location) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return at.UTC(), nil
	}
	day, err := time.ParseInLocation(time.DateOnly, raw, location)
	if err != nil {
		return time.Time{}, err
	}
	return day.UTC(), nil
}
