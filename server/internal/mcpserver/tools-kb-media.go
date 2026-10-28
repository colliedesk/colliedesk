package mcpserver

import (
	"net/http"
	"net/url"
	"strconv"
)

type restKBMedia struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	ContentType  string          `json:"content_type"`
	Size         int64           `json:"size"`
	URL          string          `json:"url"`
	Name         string          `json:"name"`
	Alt          string          `json:"alt"`
	Width        *int            `json:"width"`
	Height       *int            `json:"height"`
	DurationMS   *int64          `json:"duration_ms"`
	ThumbnailURL *string         `json:"thumbnail_url"`
	PosterURL    *string         `json:"poster_url"`
	Codecs       []string        `json:"codecs"`
	Kept         bool            `json:"kept"`
	DeletedAt    *string         `json:"deleted_at"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
	Usage        *map[string]any `json:"usage"`
	PurgeAt      *string         `json:"purge_at"`
}

func kbMediaView(media restKBMedia) map[string]any {
	view := map[string]any{"id": media.ID, "kind": media.Kind, "content_type": media.ContentType, "size": media.Size, "url": media.URL,
		"width": media.Width, "height": media.Height, "has_thumbnail": media.ThumbnailURL != nil, "kept": media.Kept, "in_trash": media.DeletedAt != nil,
		"created_at": media.CreatedAt, "updated_at": media.UpdatedAt, "team_text": map[string]any{"name": media.Name, "alt": media.Alt}}
	if media.Kind == "video" {
		view["duration_ms"], view["codecs"], view["has_poster"] = media.DurationMS, media.Codecs, media.PosterURL != nil
	}
	if media.Usage != nil {
		view["usage"] = *media.Usage
	}
	if media.PurgeAt != nil {
		view["purge_at"] = *media.PurgeAt
	}
	return view
}

func (c *call) forMedia(id string) {
	c.notFound = "No knowledge base media " + id + " in this workspace. Use an id from colliedesk_kb_media_list, or the id in an article's /api/v1/kb/images/<id> link."
}

func kbMediaPath(id string, rest ...string) string {
	path := "/api/v1/kb/media/" + url.PathEscape(id)
	for _, part := range rest {
		path += "/" + part
	}
	return path
}

func kbMediaList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Kind   string `json:"kind"`
		Query  string `json:"query"`
		Usage  string `json:"usage"`
		Sort   string `json:"sort"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for key, value := range map[string]string{"kind": args.Kind, "q": args.Query, "usage": args.Usage, "sort": args.Sort, "cursor": args.Cursor} {
		if value != "" {
			query.Set(key, value)
		}
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	var page struct {
		Items      []restKBMedia   `json:"items"`
		NextCursor *string         `json:"next_cursor"`
		Summary    *map[string]any `json:"summary"`
	}
	if err := c.get("/api/v1/kb/media", query, &page); err != nil {
		return nil, err
	}
	media := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		media = append(media, kbMediaView(item))
	}
	out := map[string]any{"media": media, "next_cursor": page.NextCursor}
	if page.Summary != nil {
		out["summary"] = *page.Summary
	}
	return out, nil
}

// kbThumbnailLimit is the most a thumbnail download reads.
const kbThumbnailLimit = 1 << 20

func kbMediaGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"media_id"`
		Thumbnail *bool  `json:"thumbnail"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forMedia(args.ID)
	var details struct {
		restKBMedia
		Articles []struct {
			ID      string `json:"id"`
			Number  int64  `json:"number"`
			Title   string `json:"title"`
			Status  string `json:"status"`
			InTrash bool   `json:"in_trash"`
			Current bool   `json:"current"`
		} `json:"articles"`
	}
	if err := c.get(kbMediaPath(args.ID, "details"), nil, &details); err != nil {
		return nil, err
	}
	articles := make([]map[string]any, 0, len(details.Articles))
	for _, article := range details.Articles {
		articles = append(articles, map[string]any{"id": article.ID, "number": article.Number, "status": article.Status, "in_trash": article.InTrash,
			"current": article.Current, "team_text": map[string]any{"title": article.Title}})
	}
	out := map[string]any{"media": kbMediaView(details.restKBMedia), "articles": articles}
	if details.ThumbnailURL == nil || (args.Thumbnail != nil && !*args.Thumbnail) {
		return out, nil
	}
	header, body, err := c.download(kbMediaPath(args.ID, "thumbnail"), kbThumbnailLimit)
	if err != nil {
		return nil, err
	}
	return &imageResult{Data: body, MIMEType: header.Get("Content-Type"), Meta: out}, nil
}

func kbMediaUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"media_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	change := map[string]any{}
	for key, value := range arguments {
		if key != "media_id" {
			change[key] = value
		}
	}
	if len(change) == 1 {
		return nil, &toolError{"Give something to change: name, alt or kept."}
	}
	c.forMedia(args.ID)
	var updated restKBMedia
	if err := c.send(http.MethodPatch, kbMediaPath(args.ID), change, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"media": kbMediaView(updated)}, nil
}

type restKBMediaResults struct {
	Results []map[string]any `json:"results"`
	BatchID *string          `json:"batch_id"`
}

func kbMediaTrash(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		IDs         []string `json:"media_ids"`
		AllUnused   bool     `json:"all_unused"`
		Before      string   `json:"before"`
		IncludeKept bool     `json:"include_kept"`
		BatchID     string   `json:"batch_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.AllUnused == (len(args.IDs) > 0) {
		return nil, &toolError{"Give media_ids, or all_unused with before, not both."}
	}
	if args.AllUnused {
		if args.Before == "" {
			return nil, &toolError{"With all_unused, give before: when you listed the items (RFC 3339), so nothing added since is moved."}
		}
		body := map[string]any{"before": args.Before, "include_kept": args.IncludeKept}
		if args.BatchID != "" {
			body["batch_id"] = args.BatchID
		}
		var moved struct {
			Trashed int     `json:"trashed"`
			More    bool    `json:"more"`
			BatchID *string `json:"batch_id"`
		}
		if err := c.send(http.MethodPost, "/api/v1/kb/media/trash-unused", body, &moved); err != nil {
			return nil, err
		}
		return map[string]any{"trashed": moved.Trashed, "more": moved.More, "batch_id": moved.BatchID}, nil
	}
	c.notFound = "No knowledge base media trash batch " + args.BatchID + " in this workspace."
	body := map[string]any{"ids": args.IDs}
	if args.BatchID != "" {
		body["batch_id"] = args.BatchID
	}
	var out restKBMediaResults
	if err := c.send(http.MethodPost, "/api/v1/kb/media/trash", body, &out); err != nil {
		return nil, err
	}
	return map[string]any{"results": out.Results, "batch_id": out.BatchID}, nil
}

func kbMediaTrashList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	var page struct {
		Items      []restKBMedia `json:"items"`
		NextCursor *string       `json:"next_cursor"`
	}
	if err := c.get("/api/v1/kb/media/trash", query, &page); err != nil {
		return nil, err
	}
	media := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		media = append(media, kbMediaView(item))
	}
	return map[string]any{"media": media, "next_cursor": page.NextCursor}, nil
}

func kbMediaUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		IDs     []string `json:"media_ids"`
		BatchID string   `json:"batch_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if (args.BatchID == "") == (len(args.IDs) == 0) {
		return nil, &toolError{"Give media_ids or batch_id, not both."}
	}
	body := map[string]any{}
	if args.BatchID != "" {
		body["batch_id"] = args.BatchID
		c.notFound = "No knowledge base media trash batch " + args.BatchID + " in this workspace. Use the batch_id colliedesk_kb_media_trash gave."
	} else {
		body["ids"] = args.IDs
	}
	var out restKBMediaResults
	if err := c.send(http.MethodPost, "/api/v1/kb/media/restore", body, &out); err != nil {
		return nil, err
	}
	return map[string]any{"results": out.Results}, nil
}

func kbMediaPurge(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		IDs []string `json:"media_ids"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var out restKBMediaResults
	if err := c.send(http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": args.IDs}, &out); err != nil {
		return nil, err
	}
	return map[string]any{"results": out.Results}, nil
}

type restKBMediaReplacement struct {
	ID           string           `json:"id"`
	From         string           `json:"from_media_id"`
	To           string           `json:"to_media_id"`
	ArticleIDs   []string         `json:"article_ids"`
	Translations []map[string]any `json:"translations"`
	UndoneAt     *string          `json:"undone_at"`
}

func kbMediaReplacementView(replacement restKBMediaReplacement) map[string]any {
	return map[string]any{"replacement_id": replacement.ID, "from_media_id": replacement.From, "to_media_id": replacement.To,
		"article_ids": replacement.ArticleIDs, "translations": replacement.Translations, "undone": replacement.UndoneAt != nil}
}

func kbMediaReplace(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string `json:"media_id"`
		With string `json:"with_media_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base media " + args.ID + " or " + args.With + " in this workspace. Use ids from colliedesk_kb_media_list."
	var replacement restKBMediaReplacement
	if err := c.send(http.MethodPost, kbMediaPath(args.ID, "replace"), map[string]any{"with_media_id": args.With}, &replacement); err != nil {
		return nil, err
	}
	return kbMediaReplacementView(replacement), nil
}

func kbMediaReplaceUndo(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"replacement_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge base media replacement " + args.ID + " in this workspace. Use the replacement_id colliedesk_kb_media_replace gave."
	var replacement restKBMediaReplacement
	if err := c.send(http.MethodPost, "/api/v1/kb/media-replacements/"+url.PathEscape(args.ID)+"/undo", nil, &replacement); err != nil {
		return nil, err
	}
	return kbMediaReplacementView(replacement), nil
}
