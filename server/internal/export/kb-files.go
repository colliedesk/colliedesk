package export

import "time"

// KBFile is a file of the knowledge base media library (a PDF, an Office or OpenDocument file, CSV,
// text, RTF or an archive); its bytes are the file at Path. Its extracted text is not exported: the
// server reads it again from the bytes.
type KBFile struct {
	ID               string     `json:"id"`
	ContentType      string     `json:"content_type"`
	Size             int64      `json:"size"`
	SHA256           string     `json:"sha256"`
	Path             string     `json:"path"`
	Name             string     `json:"name"`
	Kept             bool       `json:"kept"`
	CreatedByAgentID *string    `json:"created_by_agent_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	UpdatedByAgentID *string    `json:"updated_by_agent_id"`
	DeletedAt        *time.Time `json:"deleted_at"`
	DeletedByAgentID *string    `json:"deleted_by_agent_id"`
	StorageKey       string     `json:"-"`
}

// KBArticleAttachment is one file an article lists under its body (Locale nil) or one a translation
// lists, at Position in that list.
type KBArticleAttachment struct {
	ArticleID string    `json:"article_id"`
	Locale    *string   `json:"locale"`
	MediaID   string    `json:"media_id"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

var kbFileExtensions = map[string]string{
	"application/pdf": ".pdf", "application/msword": ".doc", "application/vnd.ms-excel": ".xls", "application/vnd.ms-powerpoint": ".ppt",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.oasis.opendocument.text":                                   ".odt",
	"application/vnd.oasis.opendocument.spreadsheet":                            ".ods",
	"application/vnd.oasis.opendocument.presentation":                           ".odp",
	"text/csv": ".csv", "text/plain": ".txt", "application/rtf": ".rtf", "application/zip": ".zip",
	"application/x-7z-compressed": ".7z", "application/gzip": ".tar.gz",
}

// KBFile records a knowledge base file; Finish copies its bytes to kb-files/<id>.<ext> with the
// attachments, checked against the same size and SHA-256.
func (w *Writer) KBFile(record KBFile) error {
	if w.err != nil {
		return w.err
	}
	record.Path = "kb-files/" + record.ID + kbFileExtensions[record.ContentType]
	if err := w.open("kb-files.ndjson"); err != nil {
		return err
	}
	if err := w.spillFile(spillRecord{ID: record.ID, Key: record.StorageKey, Path: record.Path, Size: record.Size, SHA256: record.SHA256}); err != nil {
		return err
	}
	return w.record("kb-files.ndjson", record)
}

func (w *Writer) KBArticleAttachment(record KBArticleAttachment) error {
	return w.record("kb-article-attachments.ndjson", record)
}
