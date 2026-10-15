package export

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const (
	Format        = "colliedesk-export"
	FormatVersion = 1

	flushEvery = 100
	maxName    = 200
)

// Record files in archive order. A record of an earlier file after a later one is a programming
// error, because a ZIP entry is written in one piece.
var recordFiles = []string{"agents.ndjson", "contacts.ndjson", "tags.ndjson", "conversations.ndjson"}

var countKeys = map[string]string{
	"agents.ndjson": "agents", "contacts.ndjson": "contacts", "tags.ndjson": "tags", "conversations.ndjson": "conversations",
}

// Opener reads stored attachment bytes; attachments.Storage satisfies it.
type Opener interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

type fileRef struct {
	attachment Attachment
}

// Writer builds the archive on out. It is not safe for concurrent use.
type Writer struct {
	zip         *zip.Writer
	out         io.Writer
	opener      Opener
	workspaceID string
	exportedAt  time.Time
	workspace   domain.Workspace

	current    int // index into recordFiles of the open entry, -1 before the first
	entry      io.Writer
	counts     map[string]int
	files      []fileRef
	sinceFlush int
	err        error
}

// NewWriter starts an archive for the workspace. opener may be nil when no attachment storage is
// configured; an export that then meets an attachment fails.
func NewWriter(out io.Writer, opener Opener, workspaceID string, exportedAt time.Time) *Writer {
	return &Writer{
		zip: zip.NewWriter(out), out: out, opener: opener, workspaceID: workspaceID,
		exportedAt: exportedAt.UTC(), current: -1,
		counts: map[string]int{"agents": 0, "contacts": 0, "tags": 0, "conversations": 0, "messages": 0, "attachments": 0, "time_entries": 0},
	}
}

func (w *Writer) Workspace(workspace domain.Workspace) error {
	w.workspace = workspace
	return nil
}

func (w *Writer) Agent(record Agent) error     { return w.record("agents.ndjson", record) }
func (w *Writer) Contact(record Contact) error { return w.record("contacts.ndjson", record) }
func (w *Writer) Tag(record Tag) error         { return w.record("tags.ndjson", record) }

func (w *Writer) Conversation(record Conversation) error {
	if w.err != nil {
		return w.err
	}
	if record.TagIDs == nil {
		record.TagIDs = []string{}
	}
	if record.Messages == nil {
		record.Messages = []Message{}
	}
	if record.TimeEntries == nil {
		record.TimeEntries = []domain.TimeEntry{}
	}
	for m := range record.Messages {
		message := &record.Messages[m]
		if message.Attachments == nil {
			message.Attachments = []Attachment{}
		}
		for a := range message.Attachments {
			item := &message.Attachments[a]
			item.Path = "attachments/" + item.ID + "/" + SanitizeFilename(item.Filename)
			w.files = append(w.files, fileRef{attachment: *item})
			w.counts["attachments"]++
		}
		w.counts["messages"]++
	}
	w.counts["time_entries"] += len(record.TimeEntries)
	return w.record("conversations.ndjson", record)
}

// Flush pushes what is written so far to the client.
func (w *Writer) Flush() error {
	if err := w.zip.Flush(); err != nil {
		return w.fail(fmt.Errorf("flush archive: %w", err))
	}
	if flusher, ok := w.out.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	return nil
}

func (w *Writer) record(name string, value any) error {
	if w.err != nil {
		return w.err
	}
	if err := w.open(name); err != nil {
		return err
	}
	encoder := json.NewEncoder(w.entry)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return w.fail(fmt.Errorf("write %s: %w", name, err))
	}
	w.counts[countKeys[name]]++
	if name == "conversations.ndjson" {
		w.sinceFlush++
		if w.sinceFlush >= flushEvery {
			w.sinceFlush = 0
			return w.Flush()
		}
	}
	return nil
}

func (w *Writer) open(name string) error {
	index := -1
	for position, candidate := range recordFiles {
		if candidate == name {
			index = position
		}
	}
	if index < w.current {
		return w.fail(fmt.Errorf("export order: %s after %s", name, recordFiles[w.current]))
	}
	if index == w.current {
		return nil
	}
	return w.createRecordFile(index)
}

func (w *Writer) createRecordFile(index int) error {
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: recordFiles[index], Method: zip.Deflate, Modified: w.exportedAt})
	if err != nil {
		return w.fail(fmt.Errorf("create %s: %w", recordFiles[index], err))
	}
	w.current, w.entry = index, entry
	if index > 0 {
		return w.Flush()
	}
	return nil
}

func (w *Writer) fail(err error) error {
	w.err = err
	return err
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	ExportedAt time.Time        `json:"exported_at"`
	Workspace  domain.Workspace `json:"workspace"`
	Counts     map[string]int   `json:"counts"`
	Files      []manifestFile   `json:"files"`
}

// Finish writes the attachment files, then the manifest, then the ZIP directory. It stops at the
// first file that cannot be read or does not match its recorded checksum, and then writes no
// manifest, so an incomplete export never looks complete.
func (w *Writer) Finish(ctx context.Context) error {
	if w.err != nil {
		return w.err
	}
	for index := w.current + 1; index < len(recordFiles); index++ {
		if err := w.createRecordFile(index); err != nil {
			return err
		}
	}
	files := make([]manifestFile, 0, len(w.files))
	for _, ref := range w.files {
		written, err := w.writeAttachment(ctx, ref.attachment)
		if err != nil {
			return w.fail(err)
		}
		files = append(files, written)
	}
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate, Modified: w.exportedAt})
	if err != nil {
		return w.fail(fmt.Errorf("create manifest.json: %w", err))
	}
	encoder := json.NewEncoder(entry)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest{Format: Format, Version: FormatVersion, ExportedAt: w.exportedAt,
		Workspace: w.workspace, Counts: w.counts, Files: files}); err != nil {
		return w.fail(fmt.Errorf("write manifest.json: %w", err))
	}
	if err := w.zip.Close(); err != nil {
		return w.fail(fmt.Errorf("close archive: %w", err))
	}
	return nil
}

func (w *Writer) writeAttachment(ctx context.Context, item Attachment) (manifestFile, error) {
	if w.opener == nil {
		return manifestFile{}, fmt.Errorf("attachment %s: attachment storage is not configured", item.ID)
	}
	if !strings.HasPrefix(item.StorageKey, w.workspaceID+"/") {
		return manifestFile{}, fmt.Errorf("attachment %s: stored under another workspace", item.ID)
	}
	body, err := w.opener.Open(ctx, item.StorageKey)
	if err != nil {
		return manifestFile{}, fmt.Errorf("attachment %s: open stored bytes: %w", item.ID, err)
	}
	defer body.Close()
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: item.Path, Method: zip.Store, Modified: w.exportedAt})
	if err != nil {
		return manifestFile{}, fmt.Errorf("attachment %s: create entry: %w", item.ID, err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(entry, hash), body)
	if err != nil {
		return manifestFile{}, fmt.Errorf("attachment %s: copy: %w", item.ID, err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if size != item.Size || sum != item.SHA256 {
		return manifestFile{}, fmt.Errorf("attachment %s: stored bytes (%d bytes, sha256 %s) differ from the recorded %d bytes, sha256 %s",
			item.ID, size, sum, item.Size, item.SHA256)
	}
	return manifestFile{Path: item.Path, Size: size, SHA256: sum}, nil
}

// SanitizeFilename makes a stored filename safe as the last element of an archive path: no
// directories, no control characters, never empty, never "." or "..".
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if len(name) > maxName {
		cut := maxName
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	return name
}
