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
var recordFiles = []string{"agents.ndjson", "contacts.ndjson", "contact-merges.ndjson", "tags.ndjson", "saved-replies.ndjson", "saved-reply-versions.ndjson", "saved-views.ndjson",
	"kb-collections.ndjson", "kb-articles.ndjson", "kb-article-versions.ndjson", "kb-article-translations.ndjson", "kb-images.ndjson", "kb-videos.ndjson", "kb-media-replacements.ndjson", "help-center.ndjson",
	"agent-requests.ndjson", "import-jobs.ndjson", "import-people.ndjson",
	"ai-providers.ndjson", "ai-slots.ndjson", "ai-settings-events.ndjson", "ai-source-consents.ndjson", "ai-usage.ndjson", "ai-drafts.ndjson", "ai-triage.ndjson", "ai-chat-sessions.ndjson", "results-baselines.ndjson",
	"autofix-installations.ndjson", "autofix-repos.ndjson", "autofix-connectors.ndjson", "fixes.ndjson", "fix-events.ndjson",
	"channels.ndjson", "channel-events.ndjson", "channel-ai-levels.ndjson", "channel-inbound-events.ndjson", "channel-threads.ndjson", "channel-thread-items.ndjson", "conversation-thread-items.ndjson", "intake-gatherings.ndjson", "intake-decisions.ndjson", "intake-eval-runs.ndjson", "message-moves.ndjson", "conversations.ndjson"}

var countKeys = map[string]string{
	"agents.ndjson": "agents", "contacts.ndjson": "contacts", "contact-merges.ndjson": "contact_merges", "tags.ndjson": "tags", "saved-replies.ndjson": "saved_replies", "saved-reply-versions.ndjson": "saved_reply_versions", "saved-views.ndjson": "saved_views",
	"kb-collections.ndjson": "kb_collections", "kb-articles.ndjson": "kb_articles", "kb-article-versions.ndjson": "kb_article_versions",
	"kb-article-translations.ndjson": "kb_article_translations", "kb-images.ndjson": "kb_images", "kb-videos.ndjson": "kb_videos", "kb-media-replacements.ndjson": "kb_media_replacements", "help-center.ndjson": "help_center", "agent-requests.ndjson": "agent_requests", "import-jobs.ndjson": "import_jobs", "import-people.ndjson": "import_people",
	"ai-providers.ndjson": "ai_providers", "ai-slots.ndjson": "ai_slots", "ai-settings-events.ndjson": "ai_settings_events", "ai-source-consents.ndjson": "ai_source_consents", "ai-usage.ndjson": "ai_usage", "ai-drafts.ndjson": "ai_drafts", "ai-triage.ndjson": "ai_triage_suggestions", "ai-chat-sessions.ndjson": "ai_chat_sessions", "results-baselines.ndjson": "results_baselines",
	"autofix-installations.ndjson": "autofix_installations", "autofix-repos.ndjson": "autofix_repos", "autofix-connectors.ndjson": "autofix_connectors", "fixes.ndjson": "fixes", "fix-events.ndjson": "fix_events",
	"channels.ndjson": "channels", "channel-events.ndjson": "channel_events", "channel-ai-levels.ndjson": "channel_ai_levels", "channel-inbound-events.ndjson": "channel_inbound_events", "channel-threads.ndjson": "channel_threads", "channel-thread-items.ndjson": "channel_thread_items", "conversation-thread-items.ndjson": "conversation_thread_items", "intake-gatherings.ndjson": "intake_gatherings", "intake-decisions.ndjson": "intake_decisions", "intake-eval-runs.ndjson": "intake_eval_runs", "message-moves.ndjson": "message_moves",
	"conversations.ndjson": "conversations",
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
		counts: map[string]int{"agents": 0, "contacts": 0, "tags": 0, "saved_replies": 0, "saved_reply_versions": 0, "saved_views": 0,
			"kb_collections": 0, "kb_articles": 0, "kb_article_versions": 0, "kb_article_translations": 0, "kb_images": 0, "kb_videos": 0, "kb_media_replacements": 0, "help_center": 0, "agent_requests": 0, "conversations": 0, "messages": 0, "attachments": 0, "time_entries": 0, "status_changes": 0},
	}
}

func (w *Writer) Workspace(workspace domain.Workspace) error {
	w.workspace = workspace
	return nil
}

func (w *Writer) Agent(record Agent) error     { return w.record("agents.ndjson", record) }
func (w *Writer) Contact(record Contact) error { return w.record("contacts.ndjson", record) }
func (w *Writer) Tag(record Tag) error         { return w.record("tags.ndjson", record) }
func (w *Writer) ContactMerge(record ContactMerge) error {
	return w.record("contact-merges.ndjson", record)
}
func (w *Writer) SavedReply(record SavedReply) error {
	return w.record("saved-replies.ndjson", record)
}
func (w *Writer) SavedReplyVersion(record SavedReplyVersion) error {
	return w.record("saved-reply-versions.ndjson", record)
}
func (w *Writer) SavedView(record SavedView) error {
	return w.record("saved-views.ndjson", record)
}
func (w *Writer) KBCollection(record KBCollection) error {
	return w.record("kb-collections.ndjson", record)
}
func (w *Writer) KBArticle(record KBArticle) error {
	if record.ImageIDs == nil {
		record.ImageIDs = []string{}
	}
	return w.record("kb-articles.ndjson", record)
}
func (w *Writer) KBArticleVersion(record KBArticleVersion) error {
	return w.record("kb-article-versions.ndjson", record)
}
func (w *Writer) KBArticleTranslation(record KBArticleTranslation) error {
	return w.record("kb-article-translations.ndjson", record)
}
func (w *Writer) KBMediaReplacement(record KBMediaReplacement) error {
	return w.record("kb-media-replacements.ndjson", record)
}
func (w *Writer) HelpCenter(record HelpCenter) error {
	return w.record("help-center.ndjson", record)
}

// KBImage records a knowledge base image; Finish copies its bytes to kb-images/<id>.<ext> with the
// attachments, checked against the same size and SHA-256.
func (w *Writer) KBImage(record KBImage) error {
	if w.err != nil {
		return w.err
	}
	record.Path = "kb-images/" + record.ID + kbImageExtensions[record.ContentType]
	if err := w.open("kb-images.ndjson"); err != nil {
		return err
	}
	if err := w.spillFile(spillRecord{ID: record.ID, Key: record.StorageKey, Path: record.Path, Size: record.Size, SHA256: record.SHA256}); err != nil {
		return err
	}
	return w.record("kb-images.ndjson", record)
}

// KBVideo records a knowledge base video; Finish copies its bytes to kb-videos/<id>.<ext> and its
// poster to kb-videos/<id>.poster.<ext>, checked against their sizes and SHA-256.
func (w *Writer) KBVideo(record KBVideo) error {
	if w.err != nil {
		return w.err
	}
	record.Path = "kb-videos/" + record.ID + map[string]string{"video/mp4": ".mp4", "video/webm": ".webm"}[record.ContentType]
	if err := w.open("kb-videos.ndjson"); err != nil {
		return err
	}
	if err := w.spillFile(spillRecord{ID: record.ID, Key: record.StorageKey, Path: record.Path, Size: record.Size, SHA256: record.SHA256}); err != nil {
		return err
	}
	if record.PosterKey != "" {
		poster := "kb-videos/" + record.ID + ".poster" + kbImageExtensions[record.PosterContentType]
		record.PosterPath = &poster
		if err := w.spillFile(spillRecord{ID: record.ID + "-poster", Key: record.PosterKey, Path: poster, Size: record.PosterSize, SHA256: record.PosterKey[len(record.PosterKey)-64:]}); err != nil {
			return err
		}
	}
	return w.record("kb-videos.ndjson", record)
}

var kbImageExtensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

func (w *Writer) AgentRequest(record AgentRequest) error {
	return w.record("agent-requests.ndjson", record)
}

func (w *Writer) ImportJob(record ImportJob) error {
	return w.record("import-jobs.ndjson", record)
}

func (w *Writer) ImportPerson(record ImportPerson) error {
	return w.record("import-people.ndjson", record)
}

func (w *Writer) AIProvider(record AIProvider) error {
	return w.record("ai-providers.ndjson", record)
}

func (w *Writer) AISlot(record AISlot) error {
	return w.record("ai-slots.ndjson", record)
}

func (w *Writer) AISettingsEvent(record AISettingsEvent) error {
	return w.record("ai-settings-events.ndjson", record)
}

func (w *Writer) AISourceConsent(record AISourceConsent) error {
	return w.record("ai-source-consents.ndjson", record)
}

func (w *Writer) IntakeGathering(record IntakeGathering) error {
	return w.record("intake-gatherings.ndjson", record)
}

func (w *Writer) IntakeDecision(record IntakeDecision) error {
	return w.record("intake-decisions.ndjson", record)
}

func (w *Writer) IntakeEvalRun(record IntakeEvalRun) error {
	return w.record("intake-eval-runs.ndjson", record)
}

func (w *Writer) MessageMove(record MessageMove) error {
	return w.record("message-moves.ndjson", record)
}

func (w *Writer) ChannelThread(record ChannelThread) error {
	return w.record("channel-threads.ndjson", record)
}

func (w *Writer) ChannelThreadItem(record ChannelThreadItem) error {
	return w.record("channel-thread-items.ndjson", record)
}

func (w *Writer) ConversationThreadItem(record ConversationThreadItem) error {
	return w.record("conversation-thread-items.ndjson", record)
}

func (w *Writer) ChannelAILevel(record ChannelAILevel) error {
	return w.record("channel-ai-levels.ndjson", record)
}

func (w *Writer) AIUsage(record AIUsage) error {
	return w.record("ai-usage.ndjson", record)
}

func (w *Writer) AIDraft(record AIDraft) error {
	return w.record("ai-drafts.ndjson", record)
}

func (w *Writer) AITriage(record AITriage) error {
	return w.record("ai-triage.ndjson", record)
}

func (w *Writer) AIChatSession(record AIChatSession) error {
	if record.Turns == nil {
		record.Turns = []AIChatTurn{}
	}
	return w.record("ai-chat-sessions.ndjson", record)
}

func (w *Writer) ResultsBaseline(record ResultsBaseline) error {
	return w.record("results-baselines.ndjson", record)
}

func (w *Writer) AutofixInstallation(record AutofixInstallation) error {
	return w.record("autofix-installations.ndjson", record)
}

func (w *Writer) AutofixRepo(record AutofixRepo) error {
	return w.record("autofix-repos.ndjson", record)
}

func (w *Writer) AutofixConnector(record AutofixConnector) error {
	return w.record("autofix-connectors.ndjson", record)
}

func (w *Writer) Fix(record Fix) error {
	return w.record("fixes.ndjson", record)
}

func (w *Writer) FixEvent(record FixEvent) error {
	return w.record("fix-events.ndjson", record)
}

func (w *Writer) Channel(record Channel) error { return w.record("channels.ndjson", record) }

func (w *Writer) ChannelEvent(record ChannelEvent) error {
	return w.record("channel-events.ndjson", record)
}

// ChannelInboundEvent writes an event and copies its stored raw payload and fetched files into the
// archive under channel-events/<id>/.
func (w *Writer) ChannelInboundEvent(record ChannelInboundEvent) error {
	if err := w.open("channel-inbound-events.ndjson"); err != nil {
		return err
	}
	if record.RawKey != "" && len(record.RawKey) >= 64 && record.RawSize > 0 {
		path := "channel-events/" + record.ID + "/raw"
		record.RawPath = &path
		if err := w.spillFile(spillRecord{ID: record.ID + "-raw", Key: record.RawKey, Path: path, Size: record.RawSize, SHA256: record.RawKey[len(record.RawKey)-64:]}); err != nil {
			return err
		}
	}
	if record.Files == nil {
		record.Files = []ChannelInboundFile{}
	}
	for index := range record.Files {
		file := &record.Files[index]
		if file.StorageKey == "" || file.Size == nil || file.SHA256 == nil {
			continue
		}
		path := fmt.Sprintf("channel-events/%s/%d-%s", record.ID, index, SanitizeFilename(file.Name))
		file.Path = &path
		if err := w.spillFile(spillRecord{ID: fmt.Sprintf("%s-%d", record.ID, index), Key: file.StorageKey, Path: path, Size: *file.Size, SHA256: *file.SHA256}); err != nil {
			return err
		}
	}
	return w.record("channel-inbound-events.ndjson", record)
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
	return w.EndConversation(record.TimeEntries, record.StatusChanges)
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

// EndConversation closes the open conversation with its time entries and status history.
func (w *Writer) EndConversation(timeEntries []domain.TimeEntry, statusChanges []StatusChange) error {
	if w.err != nil {
		return w.err
	}
	if !w.inThread {
		return w.fail(errors.New("export order: no conversation to end"))
	}
	if timeEntries == nil {
		timeEntries = []domain.TimeEntry{}
	}
	if statusChanges == nil {
		statusChanges = []StatusChange{}
	}
	encoded, err := marshal(timeEntries)
	if err != nil {
		return w.fail(fmt.Errorf("encode time entries: %w", err))
	}
	changes, err := marshal(statusChanges)
	if err != nil {
		return w.fail(fmt.Errorf("encode status changes: %w", err))
	}
	w.inThread = false
	w.counts["time_entries"] += len(timeEntries)
	w.counts["status_changes"] += len(statusChanges)
	w.counts["conversations"]++
	line := append(append(append([]byte(`],"time_entries":`), encoded...), `,"status_changes":`...), changes...)
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
