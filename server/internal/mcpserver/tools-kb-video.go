package mcpserver

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// kbVideoPartLimit is the most bytes a part sent through a tool carries: 5 MiB is under 7,000,000
// base64 characters, the most a tool argument holds.
const kbVideoPartLimit = 5 << 20

type restKBUpload struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Size      int64   `json:"size"`
	Offset    int64   `json:"offset"`
	ExpiresAt string  `json:"expires_at"`
	MediaID   *string `json:"media_id"`
	CreatedBy *struct {
		Name string `json:"name"`
	} `json:"created_by"`
}

func kbUploadView(upload restKBUpload) map[string]any {
	return map[string]any{"upload_id": upload.ID, "size": upload.Size, "offset": upload.Offset, "expires_at": upload.ExpiresAt, "media_id": upload.MediaID}
}

func kbUploadPath(id string, rest ...string) string {
	path := "/api/v1/kb/media-uploads/" + url.PathEscape(id)
	for _, part := range rest {
		path += "/" + part
	}
	return path
}

func (c *call) forUpload(id string) {
	c.notFound = "No knowledge base video upload " + id + " in this workspace; it may have expired after 24 hours. Start again with colliedesk_kb_video_upload_start."
}

func kbVideoUploadStart(c *call, arguments map[string]any) (any, error) {
	var upload restKBUpload
	if err := c.send(http.MethodPost, "/api/v1/kb/media-uploads", arguments, &upload); err != nil {
		return nil, err
	}
	return kbUploadView(upload), nil
}

func kbVideoUploadPart(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID     string `json:"upload_id"`
		Offset int64  `json:"offset"`
		Data   string `json:"data_base64"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args.Data))
	if err != nil {
		return nil, &toolError{"data_base64 is not standard base64: " + err.Error()}
	}
	if len(data) == 0 || len(data) > kbVideoPartLimit {
		return nil, &toolError{"A part holds 1 byte to 5 MiB."}
	}
	c.forUpload(args.ID)
	var status restKBUpload
	if err := c.get(kbUploadPath(args.ID), nil, &status); err != nil {
		return nil, err
	}
	headers := map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", args.Offset, args.Offset+int64(len(data))-1, status.Size)}
	var upload restKBUpload
	if err := c.sendBytes(http.MethodPut, kbUploadPath(args.ID), "application/octet-stream", headers, data, &upload); err != nil {
		return nil, err
	}
	return kbUploadView(upload), nil
}

func kbVideoUploadStatus(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"upload_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forUpload(args.ID)
	var upload restKBUpload
	if err := c.get(kbUploadPath(args.ID), nil, &upload); err != nil {
		return nil, err
	}
	return kbUploadView(upload), nil
}

func kbVideoUploadList(c *call, _ map[string]any) (any, error) {
	var page struct {
		Items []restKBUpload `json:"items"`
	}
	if err := c.get("/api/v1/kb/media-uploads", nil, &page); err != nil {
		return nil, err
	}
	uploads := make([]map[string]any, 0, len(page.Items))
	for _, upload := range page.Items {
		view := kbUploadView(upload)
		view["name"] = upload.Name
		if upload.CreatedBy != nil {
			view["started_by"] = upload.CreatedBy.Name
		}
		uploads = append(uploads, view)
	}
	return map[string]any{"uploads": uploads}, nil
}

func kbVideoUploadCancel(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"upload_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forUpload(args.ID)
	if err := c.send(http.MethodDelete, kbUploadPath(args.ID), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": true}, nil
}

func kbVideoUploadComplete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"upload_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forUpload(args.ID)
	var media restKBMedia
	if err := c.send(http.MethodPost, kbUploadPath(args.ID, "complete"), nil, &media); err != nil {
		return nil, err
	}
	return map[string]any{"media": kbMediaView(media)}, nil
}

func kbVideoPosterSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string `json:"media_id"`
		Data string `json:"data_base64"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args.Data))
	if err != nil {
		return nil, &toolError{"data_base64 is not standard base64: " + err.Error()}
	}
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/png"
	}
	c.forMedia(args.ID)
	var media restKBMedia
	if err := c.sendBytes(http.MethodPut, kbMediaPath(args.ID, "poster"), contentType, nil, data, &media); err != nil {
		return nil, err
	}
	return map[string]any{"media": kbMediaView(media)}, nil
}
