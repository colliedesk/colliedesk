package domain

import (
	"encoding/json"
	"errors"
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
