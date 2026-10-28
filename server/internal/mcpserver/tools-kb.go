package mcpserver

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type restKBCollection struct {
	ID           string      `json:"id"`
	ParentID     *string     `json:"parent_id"`
	Name         string      `json:"name"`
	Slug         string      `json:"slug"`
	Description  string      `json:"description"`
	Position     int         `json:"position"`
	ArticleCount int         `json:"article_count"`
	UpdatedAt    string      `json:"updated_at"`
	DeletedAt    string      `json:"deleted_at,omitempty"`
	DeletedBy    *restPerson `json:"deleted_by"`
	PurgesAt     string      `json:"purges_at,omitempty"`
}

type kbCollectionOut struct {
	ID           string  `json:"id"`
	ParentID     *string `json:"parent_id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Description  string  `json:"description"`
	Position     int     `json:"position"`
	ArticleCount int     `json:"article_count"`
	UpdatedAt    string  `json:"updated_at"`
	DeletedAt    string  `json:"deleted_at,omitempty"`
	DeletedBy    *string `json:"deleted_by,omitempty"`
	PurgesAt     string  `json:"purges_at,omitempty"`
}

func kbCollectionView(collection restKBCollection) kbCollectionOut {
	out := kbCollectionOut{ID: collection.ID, ParentID: collection.ParentID, Name: collection.Name, Slug: collection.Slug, Description: collection.Description,
		Position: collection.Position, ArticleCount: collection.ArticleCount, UpdatedAt: collection.UpdatedAt, DeletedAt: collection.DeletedAt, PurgesAt: collection.PurgesAt}
	if collection.DeletedBy != nil {
		out.DeletedBy = &collection.DeletedBy.Name
	}
	return out
}

// restKBArticle is an article, a listed or trashed one, or a version: the fields each answer has.
type restKBArticle struct {
	ID            string                `json:"id"`
	Number        int64                 `json:"number"`
	CollectionID  string                `json:"collection_id"`
	Position      int                   `json:"position"`
	Title         string                `json:"title"`
	Slug          string                `json:"slug"`
	Status        string                `json:"status"`
	PublishedAt   *string               `json:"published_at"`
	UpdatedAt     string                `json:"updated_at"`
	LastChange    *restSavedReplyChange `json:"last_change"`
	URL           string                `json:"url"`
	BodyMD        string                `json:"body_md"`
	SEOTitle      string                `json:"seo_title"`
	SEODesc       string                `json:"seo_description"`
	HasSourceHTML bool                  `json:"has_source_html"`
	Translations  []struct {
		Locale string `json:"locale"`
	} `json:"translations"`
	DeletedAt   *string     `json:"deleted_at"`
	DeletedBy   *restPerson `json:"deleted_by"`
	PurgesAt    string      `json:"purges_at"`
	AIExclusion *struct {
		At string      `json:"at"`
		By *restPerson `json:"by"`
	} `json:"ai_exclusion"`
}

type kbTitle struct {
	Title string `json:"title"`
}

type kbArticleRowOut struct {
	ID           string               `json:"id"`
	Number       int64                `json:"number"`
	CollectionID string               `json:"collection_id"`
	Position     int                  `json:"position"`
	Status       string               `json:"status"`
	Slug         string               `json:"slug"`
	PublishedAt  *string              `json:"published_at"`
	UpdatedAt    string               `json:"updated_at"`
	LastChange   *savedReplyChangeOut `json:"last_change"`
	URL          string               `json:"url"`
	TeamText     kbTitle              `json:"team_text"`
	DeletedAt    string               `json:"deleted_at,omitempty"`
	DeletedBy    *string              `json:"deleted_by,omitempty"`
	PurgesAt     string               `json:"purges_at,omitempty"`
}

func kbArticleRow(article restKBArticle) kbArticleRowOut {
	out := kbArticleRowOut{ID: article.ID, Number: article.Number, CollectionID: article.CollectionID, Position: article.Position, Status: article.Status, Slug: article.Slug,
		PublishedAt: article.PublishedAt, UpdatedAt: article.UpdatedAt, LastChange: changeOut(article.LastChange), URL: article.URL, TeamText: kbTitle{article.Title}, PurgesAt: article.PurgesAt}
	if article.DeletedAt != nil {
		out.DeletedAt = *article.DeletedAt
	}
	if article.DeletedBy != nil {
		out.DeletedBy = &article.DeletedBy.Name
	}
	return out
}

type kbArticleOut struct {
	ID            string               `json:"id"`
	Number        int64                `json:"number"`
	CollectionID  string               `json:"collection_id"`
	Status        string               `json:"status"`
	Slug          string               `json:"slug"`
	PublishedAt   *string              `json:"published_at"`
	UpdatedAt     string               `json:"updated_at"`
	LastChange    *savedReplyChangeOut `json:"last_change"`
	URL           string               `json:"url"`
	TeamText      kbTeamText           `json:"team_text"`
	InTrash       bool                 `json:"in_trash"`
	HasSourceHTML bool                 `json:"has_source_html"`
	AIExclusion   *kbAIExclusionOut    `json:"ai_exclusion"`
	Translations  []string             `json:"translations"`
}

// kbTeamText is what the team wrote of an article: its title, text and search fields.
type kbTeamText struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	SEOTitle       string `json:"seo_title"`
	SEODescription string `json:"seo_description"`
}

// kbAIExclusionOut is since when, and by whom, AI may not read an article.
type kbAIExclusionOut struct {
	At string  `json:"at"`
	By *string `json:"by"`
}

func kbArticleView(article restKBArticle) map[string]any {
	out := kbArticleOut{ID: article.ID, Number: article.Number, CollectionID: article.CollectionID, Status: article.Status, Slug: article.Slug,
		PublishedAt: article.PublishedAt, UpdatedAt: article.UpdatedAt, LastChange: changeOut(article.LastChange), URL: article.URL, TeamText: kbTeamText{article.Title, article.BodyMD, article.SEOTitle, article.SEODesc},
		InTrash: article.DeletedAt != nil, HasSourceHTML: article.HasSourceHTML, Translations: []string{}}
	for _, translation := range article.Translations {
		out.Translations = append(out.Translations, translation.Locale)
	}
	if article.AIExclusion != nil {
		out.AIExclusion = &kbAIExclusionOut{At: article.AIExclusion.At}
		if article.AIExclusion.By != nil {
			out.AIExclusion.By = &article.AIExclusion.By.Name
		}
	}
	return map[string]any{"article": out}
}

func kbArticlePath(id string, rest ...string) string {
	return "/api/v1/kb/articles/" + url.PathEscape(id) + strings.Join(rest, "")
}

func kbCollectionPath(id string, rest ...string) string {
	return "/api/v1/kb/collections/" + url.PathEscape(id) + strings.Join(rest, "")
}

func (c *call) forArticle(id string) {
	c.notFound = "No knowledge base article " + id + " in this workspace. Find it with colliedesk_kb_articles_search or colliedesk_kb_articles_trash."
}

func (c *call) forCollection(id string) {
	c.notFound = "No knowledge base collection " + id + " in this workspace. List them with colliedesk_kb_collections_list or colliedesk_kb_collections_trash."
}

func kbCollectionsOut(c *call, path string) (any, error) {
	var page struct {
		Items []restKBCollection `json:"items"`
	}
	if err := c.get(path, nil, &page); err != nil {
		return nil, err
	}
	out := make([]kbCollectionOut, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, kbCollectionView(item))
	}
	return map[string]any{"collections": out}, nil
}

func kbCollectionsList(c *call, _ map[string]any) (any, error) {
	return kbCollectionsOut(c, "/api/v1/kb/collections")
}

func kbCollectionsTrash(c *call, _ map[string]any) (any, error) {
	return kbCollectionsOut(c, "/api/v1/kb/collections/trash")
}

func kbCollectionCreate(c *call, arguments map[string]any) (any, error) {
	var created restKBCollection
	if err := c.send(http.MethodPost, "/api/v1/kb/collections", arguments, &created); err != nil {
		return nil, err
	}
	return map[string]any{"collection": kbCollectionView(created)}, nil
}

func kbCollectionUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"collection_id"`
		MoveToTop bool   `json:"move_to_top"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	change := map[string]any{}
	for key, value := range arguments {
		if key != "collection_id" && key != "move_to_top" {
			change[key] = value
		}
	}
	if args.MoveToTop {
		if _, given := change["parent_id"]; given {
			return nil, &toolError{"Give parent_id or move_to_top, not both."}
		}
		change["parent_id"] = nil
	}
	if len(change) == 1 {
		return nil, &toolError{"Give something to change: name, slug, description, position, parent_id or move_to_top."}
	}
	c.forCollection(args.ID)
	var updated restKBCollection
	if err := c.send(http.MethodPatch, kbCollectionPath(args.ID), change, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"collection": kbCollectionView(updated)}, nil
}

func kbCollectionDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"collection_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forCollection(args.ID)
	if err := c.send(http.MethodDelete, kbCollectionPath(args.ID, "?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"collection_id": args.ID, "in_trash": true,
		"next": "The collection is in the trash for 30 days. Bring it back with colliedesk_kb_collection_undelete (collection_id " + args.ID + ")."}, nil
}

func kbCollectionUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"collection_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base collection " + args.ID + " in the trash of this workspace. List the trash with colliedesk_kb_collections_trash."
	var restored restKBCollection
	if err := c.send(http.MethodPost, kbCollectionPath(args.ID, "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return map[string]any{"collection": kbCollectionView(restored)}, nil
}

// kbArticlePage reads one page of a list of articles.
func kbArticlePage(c *call, path string, query url.Values) (any, error) {
	var page struct {
		Items      []restKBArticle `json:"items"`
		NextCursor *string         `json:"next_cursor"`
	}
	if err := c.get(path, query, &page); err != nil {
		return nil, err
	}
	out := make([]kbArticleRowOut, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, kbArticleRow(item))
	}
	return map[string]any{"articles": out, "next_cursor": page.NextCursor}, nil
}

func kbArticlesSearch(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Query        string `json:"query"`
		Status       string `json:"status"`
		CollectionID string `json:"collection_id"`
		Sort         string `json:"sort"`
		Limit        int    `json:"limit"`
		Cursor       string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for key, value := range map[string]string{"q": args.Query, "status": args.Status, "collection_id": args.CollectionID, "sort": args.Sort, "cursor": args.Cursor} {
		if value != "" {
			query.Set(key, value)
		}
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	return kbArticlePage(c, "/api/v1/kb/articles", query)
}

func kbArticlesTrash(c *call, arguments map[string]any) (any, error) {
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
	return kbArticlePage(c, "/api/v1/kb/articles/trash", query)
}

func kbArticleGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	var article restKBArticle
	if err := c.get(kbArticlePath(args.ID), nil, &article); err != nil {
		return nil, err
	}
	return kbArticleView(article), nil
}

// kbArticleFields renames the tool's body to the API's body_md.
func kbArticleFields(arguments map[string]any, skip ...string) map[string]any {
	fields := map[string]any{}
	for key, value := range arguments {
		switch {
		case key == "body":
			fields["body_md"] = value
		case !containsString(skip, key):
			fields[key] = value
		}
	}
	return fields
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func kbArticleCreate(c *call, arguments map[string]any) (any, error) {
	var created restKBArticle
	if err := c.send(http.MethodPost, "/api/v1/kb/articles", kbArticleFields(arguments), &created); err != nil {
		return nil, err
	}
	return kbArticleView(created), nil
}

func kbArticleUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	change := kbArticleFields(arguments, "article_id")
	if len(change) == 1 {
		return nil, &toolError{"Give something to change: title, body, slug, collection_id, status, seo_title or seo_description."}
	}
	c.forArticle(args.ID)
	var updated restKBArticle
	if err := c.send(http.MethodPatch, kbArticlePath(args.ID), change, &updated); err != nil {
		return nil, err
	}
	return kbArticleView(updated), nil
}

func kbArticleDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"article_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	if err := c.send(http.MethodDelete, kbArticlePath(args.ID, "?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"article_id": args.ID, "in_trash": true,
		"next": "The article is in the trash for 30 days and off the help center. Bring it back with colliedesk_kb_article_undelete (article_id " + args.ID + ")."}, nil
}

func kbArticleUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"article_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base article " + args.ID + " in the trash of this workspace. List the trash with colliedesk_kb_articles_trash."
	var restored restKBArticle
	if err := c.send(http.MethodPost, kbArticlePath(args.ID, "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return kbArticleView(restored), nil
}

func kbArticleAIExclude(c *call, arguments map[string]any) (any, error) {
	return kbArticleAIExclusion(c, arguments, http.MethodPost)
}

func kbArticleAIInclude(c *call, arguments map[string]any) (any, error) {
	return kbArticleAIExclusion(c, arguments, http.MethodDelete)
}

func kbArticleAIExclusion(c *call, arguments map[string]any, method string) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	var article restKBArticle
	if err := c.send(method, kbArticlePath(args.ID, "/ai-exclusion"), nil, &article); err != nil {
		return nil, err
	}
	return kbArticleView(article), nil
}

func kbArticleHistory(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID            string `json:"article_id"`
		Limit         int    `json:"limit"`
		BeforeVersion int    `json:"before_version"`
		Version       int    `json:"version"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
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
			Title        string  `json:"title"`
			Slug         string  `json:"slug"`
			BodyMD       string  `json:"body_md"`
			Status       string  `json:"status"`
			CollectionID *string `json:"collection_id"`
		} `json:"items"`
		NextBeforeVersion *int `json:"next_before_version"`
	}
	if err := c.get(kbArticlePath(args.ID, "/versions"), query, &page); err != nil {
		return nil, err
	}
	type versionOut struct {
		savedReplyChangeOut
		TeamText      teamText `json:"team_text"`
		BodyTruncated bool     `json:"body_truncated"`
		Status        string   `json:"status"`
		Slug          string   `json:"slug"`
		CollectionID  *string  `json:"collection_id"`
	}
	versions := make([]versionOut, 0, len(page.Items))
	for _, item := range page.Items {
		body, truncated := item.BodyMD, false
		if args.Version == 0 {
			body, truncated = cutReplyBody(body)
		}
		versions = append(versions, versionOut{*changeOut(&item.restSavedReplyChange), teamText{item.Title, body}, truncated, item.Status, item.Slug, item.CollectionID})
	}
	if args.Version > 0 {
		if len(versions) == 0 || versions[0].Version != args.Version {
			return nil, &toolError{fmt.Sprintf("Article %s has no version %d. See colliedesk_kb_article_history.", args.ID, args.Version)}
		}
		page.NextBeforeVersion = nil
	}
	return map[string]any{"article_id": args.ID, "versions": versions, "next_before_version": page.NextBeforeVersion}, nil
}

func kbArticleVersionRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"article_id"`
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base article " + args.ID + " with version " + strconv.Itoa(args.Version) + " in this workspace (an article in the trash must be undeleted first). See colliedesk_kb_article_history."
	var restored restKBArticle
	if err := c.send(http.MethodPost, kbArticlePath(args.ID, "/versions/", strconv.Itoa(args.Version), "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return kbArticleView(restored), nil
}

type restKBImage struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

func kbImageUpload(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Data string `json:"data_base64"`
		Name string `json:"name"`
		Alt  string `json:"alt"`
		Keep bool   `json:"keep"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args.Data))
	if err != nil {
		return nil, &toolError{"data_base64 is not standard base64: " + err.Error()}
	}
	// The server judges the type from the bytes; the declared one only marks the body as an image.
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/png"
	}
	var image restKBImage
	query := url.Values{}
	for key, value := range map[string]string{"name": args.Name, "alt": args.Alt} {
		if value != "" {
			query.Set(key, value)
		}
	}
	if args.Keep {
		query.Set("keep", "true")
	}
	path := "/api/v1/kb/images"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.upload(path, contentType, data, &image); err != nil {
		return nil, err
	}
	return map[string]any{"image": image}, nil
}

// kbImageLimit is the most an image download reads: the upload limit.
const kbImageLimit = 5 << 20

func kbImageGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"image_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base image " + args.ID + " in this workspace. Use the id in an article's /api/v1/kb/images/<id> link."
	header, body, err := c.download("/api/v1/kb/images/"+url.PathEscape(args.ID), kbImageLimit+1)
	if err != nil {
		return nil, err
	}
	meta := restKBImage{ID: args.ID, ContentType: header.Get("Content-Type"), Size: int64(len(body)), SHA256: header.Get("X-Content-SHA256")}
	return &imageResult{Data: body, MIMEType: meta.ContentType, Meta: map[string]any{"id": meta.ID, "content_type": meta.ContentType, "size": meta.Size, "sha256": meta.SHA256}}, nil
}

func helpCenterGet(c *call, _ map[string]any) (any, error) {
	var center map[string]any
	if err := c.get("/api/v1/help-center", nil, &center); err != nil {
		return nil, err
	}
	return map[string]any{"help_center": center}, nil
}

func helpCenterUpdate(c *call, arguments map[string]any) (any, error) {
	var center map[string]any
	if err := c.send(http.MethodPut, "/api/v1/help-center", arguments, &center); err != nil {
		return nil, err
	}
	return map[string]any{"help_center": center}, nil
}
