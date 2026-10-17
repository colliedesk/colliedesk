package export

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
var recordFiles = []string{"agents.ndjson", "contacts.ndjson", "tags.ndjson", "saved-replies.ndjson", "saved-reply-versions.ndjson", "agent-requests.ndjson", "conversations.ndjson"}

var countKeys = map[string]string{
	"agents.ndjson": "agents", "contacts.ndjson": "contacts", "tags.ndjson": "tags", "saved-replies.ndjson": "saved_replies", "saved-reply-versions.ndjson": "saved_reply_versions", "agent-requests.ndjson": "agent_requests", "conversations.ndjson": "conversations",
}

// Opener reads stored attachment bytes; attachments.Storage satisfies it.
type Opener interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// spillRecord is what Finish needs to copy one attachment file; the full metadata already went
// to the client inside conversations.ndjson.
type spillRecord struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
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
	spill      *os.File
	spillOut   *bufio.Writer
	inThread   bool
	threadSize int
	sinceFlush int
	err        error
}

// NewWriter starts an archive for the workspace. opener may be nil when no attachment storage is
// configured; an export that then meets an attachment fails.
func NewWriter(out io.Writer, opener Opener, workspaceID string, exportedAt time.Time) *Writer {
	return &Writer{
		zip: zip.NewWriter(out), out: out, opener: opener, workspaceID: workspaceID,
		exportedAt: exportedAt.UTC(), current: -1,
		counts: map[string]int{"agents": 0, "contacts": 0, "tags": 0, "saved_replies": 0, "saved_reply_versions": 0, "agent_requests": 0, "conversations": 0, "messages": 0, "attachments": 0, "time_entries": 0},
	}
}

func (w *Writer) Workspace(workspace domain.Workspace) error {
	w.workspace = workspace
	return nil
}

func (w *Writer) Agent(record Agent) error     { return w.record("agents.ndjson", record) }
func (w *Writer) Contact(record Contact) error { return w.record("contacts.ndjson", record) }
func (w *Writer) Tag(record Tag) error         { return w.record("tags.ndjson", record) }
func (w *Writer) SavedReply(record SavedReply) error {
	return w.record("saved-replies.ndjson", record)
}
func (w *Writer) SavedReplyVersion(record SavedReplyVersion) error {
	return w.record("saved-reply-versions.ndjson", record)
}
func (w *Writer) AgentRequest(record AgentRequest) error {
	return w.record("agent-requests.ndjson", record)
}

// Conversation writes a whole conversation at once; the store streams with the three calls below.
func (w *Writer) Conversation(record Conversation) error {
	if err := w.BeginConversation(record.ConversationHead); err != nil {
		return err
	}
	for _, message := range record.Messages {
		if err := w.Message(message); err != nil {
			return err
		}
	}
	return w.EndConversation(record.TimeEntries)
}

// BeginConversation starts the JSON line of one conversation. Message and EndConversation must follow.
func (w *Writer) BeginConversation(head ConversationHead) error {
	if w.err != nil {
		return w.err
	}
	if w.inThread {
		return w.fail(errors.New("export order: conversation begun inside another"))
	}
	if head.TagIDs == nil {
		head.TagIDs = []string{}
	}
	if err := w.open("conversations.ndjson"); err != nil {
		return err
	}
	encoded, err := marshal(head)
	if err != nil {
		return w.fail(fmt.Errorf("encode conversation %s: %w", head.ID, err))
	}
	// The head is a complete object; reopen it so the messages follow inside the same line.
	encoded = append(bytes.TrimSuffix(encoded, []byte("}")), []byte(`,"messages":[`)...)
	w.inThread, w.threadSize = true, 0
	return w.write(encoded)
}

// Message appends one message to the open conversation.
func (w *Writer) Message(message Message) error {
	if w.err != nil {
		return w.err
	}
	if !w.inThread {
		return w.fail(errors.New("export order: message outside a conversation"))
	}
	if message.Attachments == nil {
		message.Attachments = []Attachment{}
	}
	for index := range message.Attachments {
		item := &message.Attachments[index]
		item.Path = "attachments/" + item.ID + "/" + SanitizeFilename(item.Filename)
		if err := w.spillFile(spillRecord{ID: item.ID, Key: item.StorageKey, Path: item.Path, Size: item.Size, SHA256: item.SHA256}); err != nil {
			return err
		}
		w.counts["attachments"]++
	}
	encoded, err := marshal(message)
	if err != nil {
		return w.fail(fmt.Errorf("encode message %s: %w", message.ID, err))
	}
	if w.threadSize > 0 {
		encoded = append([]byte{','}, encoded...)
	}
	w.threadSize++
	w.counts["messages"]++
	return w.write(encoded)
}

// EndConversation closes the open conversation with its time entries.
func (w *Writer) EndConversation(timeEntries []domain.TimeEntry) error {
	if w.err != nil {
		return w.err
	}
	if !w.inThread {
		return w.fail(errors.New("export order: no conversation to end"))
	}
	if timeEntries == nil {
		timeEntries = []domain.TimeEntry{}
	}
	encoded, err := marshal(timeEntries)
	if err != nil {
		return w.fail(fmt.Errorf("encode time entries: %w", err))
	}
	w.inThread = false
	w.counts["time_entries"] += len(timeEntries)
	w.counts["conversations"]++
	line := append([]byte(`],"time_entries":`), encoded...)
	if err := w.write(append(line, '}', '\n')); err != nil {
		return err
	}
	w.sinceFlush++
	if w.sinceFlush >= flushEvery {
		w.sinceFlush = 0
		return w.Flush()
	}
	return nil
}

func marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func (w *Writer) write(data []byte) error {
	if _, err := w.entry.Write(data); err != nil {
		return w.fail(fmt.Errorf("write %s: %w", recordFiles[w.current], err))
	}
	return nil
}

func (w *Writer) spillFile(record spillRecord) error {
	if w.spill == nil {
		file, err := os.CreateTemp("", "colliedesk-export-*")
		if err != nil {
			return w.fail(fmt.Errorf("create temp file for the attachment list: %w", err))
		}
		w.spill, w.spillOut = file, bufio.NewWriter(file)
	}
	line, err := marshal(record)
	if err == nil {
		_, err = w.spillOut.Write(append(line, '\n'))
	}
	if err != nil {
		return w.fail(fmt.Errorf("record attachment %s: %w", record.ID, err))
	}
	return nil
}

// Close removes the temp file. Finish calls it; call it too when an export is abandoned.
func (w *Writer) Close() error {
	if w.spill == nil {
		return nil
	}
	name := w.spill.Name()
	w.spill.Close()
	w.spill, w.spillOut = nil, nil
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temp file: %w", err)
	}
	return nil
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
	encoded, err := marshal(value)
	if err != nil {
		return w.fail(fmt.Errorf("write %s: %w", name, err))
	}
	if err := w.write(append(encoded, '\n')); err != nil {
		return err
	}
	w.counts[countKeys[name]]++
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

type manifestHead struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	ExportedAt time.Time        `json:"exported_at"`
	Workspace  domain.Workspace `json:"workspace"`
	Counts     map[string]int   `json:"counts"`
}

// Finish writes the attachment files, then the manifest, then the ZIP directory. It stops at the
// first file that cannot be read or does not match its recorded checksum, and then writes no
// manifest, so an incomplete export never looks complete. It always removes the temp file.
func (w *Writer) Finish(ctx context.Context) error {
	defer w.Close()
	if w.err != nil {
		return w.err
	}
	if w.inThread {
		return w.fail(errors.New("export order: a conversation is still open"))
	}
	for index := w.current + 1; index < len(recordFiles); index++ {
		if err := w.createRecordFile(index); err != nil {
			return err
		}
	}
	if err := w.eachSpilled(func(record spillRecord) error { return w.writeAttachment(ctx, record) }); err != nil {
		return w.fail(err)
	}
	if err := w.writeManifest(); err != nil {
		return w.fail(err)
	}
	if err := w.zip.Close(); err != nil {
		return w.fail(fmt.Errorf("close archive: %w", err))
	}
	return nil
}

// eachSpilled replays the recorded attachments in order.
func (w *Writer) eachSpilled(fn func(spillRecord) error) error {
	if w.spill == nil {
		return nil
	}
	if err := w.spillOut.Flush(); err != nil {
		return fmt.Errorf("read back the attachment list: %w", err)
	}
	if _, err := w.spill.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("read back the attachment list: %w", err)
	}
	scanner := bufio.NewScanner(w.spill)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		var record spillRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("read back the attachment list: %w", err)
		}
		if err := fn(record); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read back the attachment list: %w", err)
	}
	return nil
}

// writeManifest streams manifest.json so a workspace with a million files needs no list in memory.
func (w *Writer) writeManifest() error {
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate, Modified: w.exportedAt})
	if err != nil {
		return fmt.Errorf("create manifest.json: %w", err)
	}
	head, err := marshal(manifestHead{Format: Format, Version: FormatVersion, ExportedAt: w.exportedAt, Workspace: w.workspace, Counts: w.counts})
	if err != nil {
		return fmt.Errorf("encode manifest.json: %w", err)
	}
	out := bufio.NewWriter(entry)
	out.Write(bytes.TrimSuffix(head, []byte("}")))
	out.WriteString(`,"files":[`)
	first := true
	err = w.eachSpilled(func(record spillRecord) error {
		line, err := marshal(manifestFile{Path: record.Path, Size: record.Size, SHA256: record.SHA256})
		if err != nil {
			return err
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		_, err = out.Write(line)
		return err
	})
	if err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}
	out.WriteString("]}\n")
	if err := out.Flush(); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}
	return nil
}

func (w *Writer) writeAttachment(ctx context.Context, item spillRecord) error {
	if w.opener == nil {
		return fmt.Errorf("attachment %s: attachment storage is not configured", item.ID)
	}
	if !strings.HasPrefix(item.Key, w.workspaceID+"/") {
		return fmt.Errorf("attachment %s: stored under another workspace", item.ID)
	}
	body, err := w.opener.Open(ctx, item.Key)
	if err != nil {
		return fmt.Errorf("attachment %s: open stored bytes: %w", item.ID, err)
	}
	defer body.Close()
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: item.Path, Method: zip.Store, Modified: w.exportedAt})
	if err != nil {
		return fmt.Errorf("attachment %s: create entry: %w", item.ID, err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(entry, hash), body)
	if err != nil {
		return fmt.Errorf("attachment %s: copy: %w", item.ID, err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if size != item.Size || sum != item.SHA256 {
		return fmt.Errorf("attachment %s: stored bytes (%d bytes, sha256 %s) differ from the recorded %d bytes, sha256 %s",
			item.ID, size, sum, item.Size, item.SHA256)
	}
	return nil
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
