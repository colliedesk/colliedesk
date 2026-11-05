package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// restKBTranslation is a translation, a listed one, or one in the trash: the fields each answer has.
type restKBTranslation struct {
	ArticleID     string                `json:"article_id"`
	Locale        string                `json:"locale"`
	Title         string                `json:"title"`
	Slug          string                `json:"slug"`
	Status        string                `json:"status"`
	PublishedAt   *string               `json:"published_at"`
	Outdated      bool                  `json:"outdated"`
	OutdatedAt    *string               `json:"outdated_at"`
	UpdatedAt     string                `json:"updated_at"`
	InTrash       bool                  `json:"in_trash"`
	LastChange    *restSavedReplyChange `json:"last_change"`
	URL           string                `json:"url"`
	BodyMD        string                `json:"body_md"`
	SEOTitle      string                `json:"seo_title"`
	SEODesc       string                `json:"seo_description"`
	LanguageOn    bool                  `json:"language_visible"`
	ArticleTitle  string                `json:"article_title"`
	ArticleNumber int64                 `json:"article_number"`
	DeletedAt     string                `json:"deleted_at"`
	DeletedBy     *restPerson           `json:"deleted_by"`
	PurgesAt      string                `json:"purges_at"`
	Original      *restKBOriginal       `json:"original"`
}

type restKBOriginal struct {
	ID             string `json:"id"`
	Number         int64  `json:"number"`
	Locale         string `json:"locale"`
	Title          string `json:"title"`
	BodyMD         string `json:"body_md"`
	SEOTitle       string `json:"seo_title"`
	SEODesc        string `json:"seo_description"`
	Status         string `json:"status"`
	CollectionName string `json:"collection_name"`
	Version        int    `json:"version"`
	UpdatedAt      string `json:"updated_at"`
}

type kbTranslationRowOut struct {
	ArticleID   string               `json:"article_id"`
	Locale      string               `json:"locale"`
	Status      string               `json:"status"`
	Outdated    bool                 `json:"outdated"`
	OutdatedAt  *string              `json:"outdated_at"`
	InTrash     bool                 `json:"in_trash"`
	Slug        string               `json:"slug"`
	PublishedAt *string              `json:"published_at"`
	UpdatedAt   string               `json:"updated_at"`
	LastChange  *savedReplyChangeOut `json:"last_change"`
	URL         string               `json:"url"`
	TeamText    kbTitle              `json:"team_text"`
}

func kbTranslationRow(item restKBTranslation) kbTranslationRowOut {
	return kbTranslationRowOut{ArticleID: item.ArticleID, Locale: item.Locale, Status: item.Status, Outdated: item.Outdated, OutdatedAt: item.OutdatedAt, InTrash: item.InTrash,
		Slug: item.Slug, PublishedAt: item.PublishedAt, UpdatedAt: item.UpdatedAt, LastChange: changeOut(item.LastChange), URL: item.URL, TeamText: kbTitle{item.Title}}
}

type kbOriginalOut struct {
	ArticleID  string     `json:"article_id"`
	Number     int64      `json:"number"`
	Locale     string     `json:"locale"`
	Status     string     `json:"status"`
	Collection string     `json:"collection"`
	Version    int        `json:"version"`
	UpdatedAt  string     `json:"updated_at"`
	TeamText   kbTeamText `json:"team_text"`
}

type kbTranslationOut struct {
	kbTranslationRowOut
	TeamText        kbTeamText    `json:"team_text"`
	LanguageVisible bool          `json:"language_visible"`
	Original        kbOriginalOut `json:"original"`
}

func kbTranslationView(item restKBTranslation) map[string]any {
	out := kbTranslationOut{kbTranslationRowOut: kbTranslationRow(item), TeamText: kbTeamText{item.Title, item.BodyMD, item.SEOTitle, item.SEODesc}, LanguageVisible: item.LanguageOn}
	if original := item.Original; original != nil {
		out.Original = kbOriginalOut{ArticleID: original.ID, Number: original.Number, Locale: original.Locale, Status: original.Status, Collection: original.CollectionName,
			Version: original.Version, UpdatedAt: original.UpdatedAt, TeamText: kbTeamText{original.Title, original.BodyMD, original.SEOTitle, original.SEODesc}}
	}
	return map[string]any{"translation": out}
}

func kbTranslationPath(articleID, locale string, rest ...string) string {
	return kbArticlePath(articleID, append([]string{"/translations/", url.PathEscape(locale)}, rest...)...)
}

func (c *call) forTranslation(articleID, locale string) {
	c.notFound = "Article " + articleID + " has no " + locale + " translation outside the trash in this workspace. colliedesk_kb_article_get lists its translations; " +
		"colliedesk_kb_translations_trash those in the trash; colliedesk_kb_translation_create adds one."
}

func helpCenterLanguagesGet(c *call, _ map[string]any) (any, error) {
	var languages map[string]any
	if err := c.get("/api/v1/help-center/languages", nil, &languages); err != nil {
		return nil, err
	}
	return map[string]any{"languages": languages}, nil
}

func helpCenterLanguagesSet(c *call, arguments map[string]any) (any, error) {
	var languages map[string]any
	if err := c.send(http.MethodPut, "/api/v1/help-center/languages", arguments, &languages); err != nil {
		return nil, err
	}
	return map[string]any{"languages": languages}, nil
}

func kbTranslationsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Locale string `json:"locale"`
		State  string `json:"state"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{"locale": {args.Locale}, "state": {args.State}}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	var page struct {
		Items []struct {
			ArticleID      string             `json:"article_id"`
			Number         int64              `json:"number"`
			Title          string             `json:"title"`
			Status         string             `json:"status"`
			CollectionID   string             `json:"collection_id"`
			CollectionName string             `json:"collection_name"`
			Translation    *restKBTranslation `json:"translation"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := c.get("/api/v1/kb/translations", query, &page); err != nil {
		return nil, err
	}
	type itemOut struct {
		ArticleID    string               `json:"article_id"`
		Number       int64                `json:"number"`
		Status       string               `json:"status"`
		CollectionID string               `json:"collection_id"`
		Collection   string               `json:"collection"`
		TeamText     kbTitle              `json:"team_text"`
		Translation  *kbTranslationRowOut `json:"translation"`
	}
	out := make([]itemOut, 0, len(page.Items))
	for _, item := range page.Items {
		row := itemOut{ArticleID: item.ArticleID, Number: item.Number, Status: item.Status, CollectionID: item.CollectionID, Collection: item.CollectionName, TeamText: kbTitle{item.Title}}
		if item.Translation != nil {
			translation := kbTranslationRow(*item.Translation)
			row.Translation = &translation
		}
		out = append(out, row)
	}
	return map[string]any{"articles": out, "next_cursor": page.NextCursor}, nil
}

func kbTranslationsTrash(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	var page struct {
		Items      []restKBTranslation `json:"items"`
		NextCursor *string             `json:"next_cursor"`
	}
	if err := c.get("/api/v1/kb/translations/trash", query, &page); err != nil {
		return nil, err
	}
	type trashedOut struct {
		kbTranslationRowOut
		ArticleTitle  string  `json:"article_title"`
		ArticleNumber int64   `json:"article_number"`
		DeletedAt     string  `json:"deleted_at"`
		DeletedBy     *string `json:"deleted_by"`
		PurgesAt      string  `json:"purges_at"`
	}
	out := make([]trashedOut, 0, len(page.Items))
	for _, item := range page.Items {
		row := trashedOut{kbTranslationRowOut: kbTranslationRow(item), ArticleTitle: item.ArticleTitle, ArticleNumber: item.ArticleNumber, DeletedAt: item.DeletedAt, PurgesAt: item.PurgesAt}
		if item.DeletedBy != nil {
			row.DeletedBy = &item.DeletedBy.Name
		}
		out = append(out, row)
	}
	return map[string]any{"translations": out, "next_cursor": page.NextCursor}, nil
}

type translationKey struct {
	ArticleID string `json:"article_id"`
	Locale    string `json:"locale"`
}

func kbTranslationCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[translationKey](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ArticleID)
	var created restKBTranslation
	if err := c.send(http.MethodPost, kbArticlePath(args.ArticleID, "/translations"), kbArticleFields(arguments, "article_id"), &created); err != nil {
		return nil, err
	}
	return kbTranslationView(created), nil
}

func kbTranslationGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[translationKey](arguments)
	if err != nil {
		return nil, err
	}
	c.forTranslation(args.ArticleID, args.Locale)
	var translation restKBTranslation
	if err := c.get(kbTranslationPath(args.ArticleID, args.Locale), nil, &translation); err != nil {
		return nil, err
	}
	return kbTranslationView(translation), nil
}

func kbTranslationUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[translationKey](arguments)
	if err != nil {
		return nil, err
	}
	change := kbArticleFields(arguments, "article_id", "locale")
	if len(change) == 1 {
		return nil, &toolError{"Give something to change: title, body, slug, status, seo_title, seo_description or outdated."}
	}
	c.forTranslation(args.ArticleID, args.Locale)
	var updated restKBTranslation
	if err := c.send(http.MethodPatch, kbTranslationPath(args.ArticleID, args.Locale), change, &updated); err != nil {
		return nil, err
	}
	return kbTranslationView(updated), nil
}

func kbTranslationDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		translationKey
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forTranslation(args.ArticleID, args.Locale)
	if err := c.send(http.MethodDelete, kbTranslationPath(args.ArticleID, args.Locale, "?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"article_id": args.ArticleID, "locale": args.Locale, "in_trash": true,
		"next": "The translation is in the trash for 30 days and off the help center; the article stays. Bring it back with colliedesk_kb_translation_undelete (article_id " +
			args.ArticleID + ", locale " + args.Locale + ")."}, nil
}

func kbTranslationUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		translationKey
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "Article " + args.ArticleID + " has no " + args.Locale + " translation in the trash of this workspace. List the trash with colliedesk_kb_translations_trash."
	var restored restKBTranslation
	if err := c.send(http.MethodPost, kbTranslationPath(args.ArticleID, args.Locale, "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return kbTranslationView(restored), nil
}

func kbTranslationHistory(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		translationKey
		Limit         int `json:"limit"`
		BeforeVersion int `json:"before_version"`
		Version       int `json:"version"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "Article " + args.ArticleID + " has no " + args.Locale + " translation in this workspace. colliedesk_kb_article_get lists its translations."
	query := url.Values{}
	switch {
	case args.Version > 0:
		query.Set("limit", "1")
		query.Set("before_version", strconv.Itoa(args.Version+1))
	default:
		if args.Limit > 0 {
			query.Set("limit", strconv.Itoa(args.Limit))
		}
		if args.BeforeVersion > 0 {
			query.Set("before_version", strconv.Itoa(args.BeforeVersion))
		}
	}
	var page struct {
		Items []struct {
			restSavedReplyChange
			Title  string `json:"title"`
			Slug   string `json:"slug"`
			BodyMD string `json:"body_md"`
			Status string `json:"status"`
		} `json:"items"`
		NextBeforeVersion *int `json:"next_before_version"`
	}
	if err := c.get(kbTranslationPath(args.ArticleID, args.Locale, "/versions"), query, &page); err != nil {
		return nil, err
	}
	type versionOut struct {
		savedReplyChangeOut
		TeamText      teamText `json:"team_text"`
		BodyTruncated bool     `json:"body_truncated"`
		Status        string   `json:"status"`
		Slug          string   `json:"slug"`
	}
	versions := make([]versionOut, 0, len(page.Items))
	for _, item := range page.Items {
		body, truncated := item.BodyMD, false
		if args.Version == 0 {
			body, truncated = cutReplyBody(body)
		}
		versions = append(versions, versionOut{*changeOut(&item.restSavedReplyChange), teamText{item.Title, body}, truncated, item.Status, item.Slug})
	}
	if args.Version > 0 {
		if len(versions) == 0 || versions[0].Version != args.Version {
			return nil, &toolError{fmt.Sprintf("The %s translation of article %s has no version %d. See colliedesk_kb_translation_history.", args.Locale, args.ArticleID, args.Version)}
		}
		page.NextBeforeVersion = nil
	}
	return map[string]any{"article_id": args.ArticleID, "locale": args.Locale, "versions": versions, "next_before_version": page.NextBeforeVersion}, nil
}

func kbTranslationVersionRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		translationKey
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "The " + args.Locale + " translation of article " + args.ArticleID + " has no version " + strconv.Itoa(args.Version) +
		" (a translation in the trash must be undeleted first). See colliedesk_kb_translation_history."
	var restored restKBTranslation
	if err := c.send(http.MethodPost, kbTranslationPath(args.ArticleID, args.Locale, "/versions/", strconv.Itoa(args.Version), "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return kbTranslationView(restored), nil
}

func kbCollectionTranslationSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID          string `json:"collection_id"`
		Locale      string `json:"locale"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forCollection(args.ID)
	var collection restKBCollection
	if err := c.send(http.MethodPut, kbCollectionPath(args.ID, "/translations/", url.PathEscape(args.Locale)), map[string]any{"name": args.Name, "description": args.Description}, &collection); err != nil {
		return nil, err
	}
	return map[string]any{"collection": kbCollectionView(collection)}, nil
}

func kbCollectionTranslationDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID     string `json:"collection_id"`
		Locale string `json:"locale"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "Collection " + args.ID + " has no name in " + args.Locale + " in this workspace (colliedesk_kb_collections_list shows its names)."
	var collection restKBCollection
	if err := c.send(http.MethodDelete, kbCollectionPath(args.ID, "/translations/", url.PathEscape(args.Locale)), nil, &collection); err != nil {
		return nil, err
	}
	return map[string]any{"collection": kbCollectionView(collection)}, nil
}
