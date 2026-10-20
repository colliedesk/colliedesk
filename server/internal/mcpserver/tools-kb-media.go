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
	Kept         bool            `json:"kept"`
	DeletedAt    *string         `json:"deleted_at"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
	Usage        *map[string]int `json:"usage"`
	PurgeAt      *string         `json:"purge_at"`
}

func kbMediaView(media restKBMedia) map[string]any {
	view := map[string]any{"id": media.ID, "kind": media.Kind, "content_type": media.ContentType, "size": media.Size, "url": media.URL,
		"width": media.Width, "height": media.Height, "has_thumbnail": media.ThumbnailURL != nil, "kept": media.Kept, "in_trash": media.DeletedAt != nil,
		"created_at": media.CreatedAt, "updated_at": media.UpdatedAt, "team_text": map[string]any{"name": media.Name, "alt": media.Alt}}
	if media.Kind == "video" {
		view["duration_ms"] = media.DurationMS
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
