package mcpserver

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// restKBAttachment is a file an article, a draft or a translation lists.
type restKBAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type kbAttachmentName struct {
	Name string `json:"name"`
}

// kbAttachmentOut puts the file name under team_text: the team chose it.
type kbAttachmentOut struct {
	ID          string           `json:"id"`
	ContentType string           `json:"content_type"`
	Size        int64            `json:"size"`
	TeamText    kbAttachmentName `json:"team_text"`
}

// kbAttachmentsView is the files in order; never null.
func kbAttachmentsView(items []restKBAttachment) []kbAttachmentOut {
	out := make([]kbAttachmentOut, 0, len(items))
	for _, item := range items {
		out = append(out, kbAttachmentOut{ID: item.ID, ContentType: item.ContentType, Size: item.Size, TeamText: kbAttachmentName{item.Name}})
	}
	return out
}

// kbFileUpload sends a file of at most one part through the upload of the media library.
func kbFileUpload(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Name string `json:"name"`
		Data string `json:"data_base64"`
		Keep bool   `json:"keep"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args.Data))
	if err != nil {
		return nil, &toolError{"data_base64 is not standard base64: " + err.Error()}
	}
	if len(data) == 0 || len(data) > kbVideoPartLimit {
		return nil, &toolError{"A file sent in one call holds 1 byte to 5 MiB; send a larger one with colliedesk_kb_video_upload_start and kind file."}
	}
	var upload restKBUpload
	start := map[string]any{"kind": "file", "name": args.Name, "size": len(data), "keep": args.Keep}
	if err := c.send(http.MethodPost, "/api/v1/kb/media-uploads", start, &upload); err != nil {
		return nil, err
	}
	c.forUpload(upload.ID)
	headers := map[string]string{"Content-Range": fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data))}
	if err := c.sendBytes(http.MethodPut, kbUploadPath(upload.ID), "application/octet-stream", headers, data, &upload); err != nil {
		return nil, err
	}
	var media restKBMedia
	if err := c.send(http.MethodPost, kbUploadPath(upload.ID, "complete"), nil, &media); err != nil {
		return nil, err
	}
	return map[string]any{"media": kbMediaView(media)}, nil
}

func kbArticleAttachmentsSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string     `json:"article_id"`
		MediaIDs  []string   `json:"media_ids"`
		Locale    string     `json:"locale"`
		UpdatedAt *time.Time `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.MediaIDs == nil {
		args.MediaIDs = []string{}
	}
	body := map[string]any{"media_ids": args.MediaIDs}
	if args.UpdatedAt != nil {
		body["updated_at"] = args.UpdatedAt
	}
	if args.Locale != "" {
		c.forTranslation(args.ID, args.Locale)
		var translation restKBTranslation
		if err := c.sendAttachments(kbTranslationPath(args.ID, args.Locale, "/attachments"), body, &translation); err != nil {
			return nil, err
		}
		return kbTranslationView(translation), nil
	}
	c.forArticle(args.ID)
	var article restKBArticle
	if err := c.sendAttachments(kbArticlePath(args.ID, "/attachments"), body, &article); err != nil {
		return nil, err
	}
	return kbArticleView(article), nil
}

var kbMediaIDInMessage = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// sendAttachments puts a list of files; a media id that is not a file is worded apart from a missing article.
func (c *call) sendAttachments(path string, body, into any) error {
	err := c.rest.do(c.ctx, c.token, http.MethodPut, path, nil, body, into)
	var failure *restError
	if errors.As(err, &failure) && failure.Status == http.StatusNotFound && failure.Code == "file_not_found" {
		return c.refusal("not_found", "The media library has no file "+kbMediaIDInMessage.FindString(failure.Message)+" outside the trash; an image or a video is not a file. "+
			"colliedesk_kb_media_list with kind=file lists the files, colliedesk_kb_media_trash_list those in the trash; colliedesk_kb_file_upload adds one. Nothing was changed.")
	}
	if err == nil {
		c.wrote = true
	}
	return c.refuse(err, c.notFound)
}
