package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

// MaxKBAttachments is the most files one article or translation lists.
const MaxKBAttachments = 50

// KBAttachment is a file of the media library that an article or a translation lists under its text.
type KBAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

// KBAttachments is the ordered list of files; it is an empty JSON array, never null.
type KBAttachments []KBAttachment

// MarshalJSON writes nil as [].
func (items KBAttachments) MarshalJSON() ([]byte, error) {
	if items == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]KBAttachment(items))
}

// ErrKBAttachmentsTooLarge means one listed file is over MaxKBFileBytes or the list comes to more
// than MaxKBArticleFileBytes.
var ErrKBAttachmentsTooLarge = errors.New("a file is over 25 MiB or the files come to more than 100 MiB")

// KBAttachmentMissingError names an id that is not a file of the workspace's library outside the
// trash: another workspace's item, an image or a video, and an unknown id all give the same error.
type KBAttachmentMissingError struct{ ID string }

func (e *KBAttachmentMissingError) Error() string {
	return "the media library has no file " + e.ID
}

// KBFileLabel is the type a person reads beside a file's name: its extension in capitals ("PDF",
// "XLSX", "TAR.GZ"), or "" for a name without one.
func KBFileLabel(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tar.gz") {
		return "TAR.GZ"
	}
	return strings.ToUpper(strings.TrimPrefix(path.Ext(lower), "."))
}

// KBFileStem is a file's name without its extension ("Warranty form.pdf" gives "Warranty form").
func KBFileStem(name string) string {
	if ext := KBFileLabel(name); ext != "" && len(ext) < len(name) {
		return name[:len(name)-len(ext)-1]
	}
	return name
}

// KBFileSlug is the last part of a file's public address: the name as a slug, then its extension
// ("Warranty form.pdf" gives "warranty-form.pdf"). Only the id finds the file; the slug is for people.
func KBFileSlug(name string) string {
	ext := strings.ToLower(KBFileLabel(name))
	slug := Slugify(KBFileStem(name), "file")
	ext = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' {
			return r
		}
		return -1
	}, ext)
	if ext == "" {
		return slug
	}
	return slug + "." + ext
}

// KBFileSize writes a file's size for a customer: "512 B", "48 KB", "2.4 MB", "20 MB" (1 KB is 1024 bytes).
func KBFileSize(size int64) string {
	switch {
	case size < 1<<10:
		return fmt.Sprintf("%d B", size)
	case size < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(size)/(1<<10))
	case size < 10<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	}
	return fmt.Sprintf("%.0f MB", float64(size)/(1<<20))
}
