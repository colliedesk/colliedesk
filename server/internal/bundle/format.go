// Package bundle imports a bundle of conversations prepared outside Colliedesk (docs/self-host/import-bundle.md):
// a ZIP holding bundle.json and the files it names. Everything is written in one transaction and
// reconciled against the counts the bundle carries: a single difference is a failure, and nothing stays.
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Format is the only bundle format this server reads.
const Format = "colliedesk-bundle/1"

// Limits of a bundle: the archive is checked whole before anything is written; only bundle.json is held
// in memory, each file is read once to check it and again as it is stored.
const (
	MaxEntries     = 200000
	MaxJSONBytes   = 256 << 20
	MaxFileBytes   = 100 << 20
	MaxTotalBytes  = 8 << 30
	maxNameRunes   = 200
	maxSourceRunes = 60
)

// Bundle is bundle.json.
type Bundle struct {
	Format        string         `json:"format"`
	Source        string         `json:"source"`
	Contacts      []Contact      `json:"contacts"`
	Conversations []Conversation `json:"conversations"`
	Counts        Counts         `json:"counts"`
}

// Contact is a customer; a contact of the workspace with one of its identities, or its email, is reused.
type Contact struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email,omitempty"`
	Organization string     `json:"organization,omitempty"`
	Identities   []Identity `json:"identities,omitempty"`
}

// Identity is how a channel of the workspace knows a contact.
type Identity struct {
	ChannelID   string `json:"channel_id"`
	Kind        string `json:"kind"`
	Value       string `json:"value"`
	DisplayName string `json:"display_name,omitempty"`
}

// Conversation is one conversation with its messages, in the order they were written.
type Conversation struct {
	ID            string   `json:"id"`
	Contact       string   `json:"contact"`
	Subject       string   `json:"subject"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeEmail string   `json:"assignee_email,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ChannelID     string   `json:"channel_id,omitempty"`
	NumberSeries  string   `json:"number_series,omitempty"`
	Number        int64    `json:"number,omitempty"`
	NumberYear    int      `json:"number_year,omitempty"`
	SLAApplies    bool     `json:"sla_applies,omitempty"`
	ThreadKeys    []string `json:"thread_keys,omitempty"`
	// Place is the chat of channel_id the conversation was opened in: its messages show there, and a
	// customer reopens it by its code from there.
	Place         *Place          `json:"place,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Origin        json.RawMessage `json:"origin,omitempty"`
	StatusChanges []StatusChange  `json:"status_changes,omitempty"`
	Messages      []Message       `json:"messages"`
}

// Place is a chat of a channel: a one-to-one chat ("direct") or a group, as the channel names it
// ("user:<id>", "group:<id>" for a Zalo OA).
type Place struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name,omitempty"`
}

// StatusChange is one change of a conversation's status.
type StatusChange struct {
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
}

// Message is a customer's message, a team reply or a note.
type Message struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Body        string    `json:"body"`
	AuthorEmail string    `json:"author_email,omitempty"`
	AuthorName  string    `json:"author_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Files       []File    `json:"files,omitempty"`
}

// File is a file of a message, stored in the archive at Path (under files/).
type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// Counts are what the system the bundle was made from holds, to reconcile against.
type Counts struct {
	Contacts      int   `json:"contacts"`
	Identities    int   `json:"identities"`
	Conversations int   `json:"conversations"`
	Incoming      int   `json:"incoming"`
	Outgoing      int   `json:"outgoing"`
	Notes         int   `json:"notes"`
	StatusChanges int   `json:"status_changes"`
	ThreadKeys    int   `json:"thread_keys"`
	Files         int   `json:"files"`
	FileBytes     int64 `json:"file_bytes"`
}

// Archive is a bundle read and checked; its files stay in the ZIP until they are stored.
type Archive struct {
	Bundle Bundle
	files  map[string]entry
}

// entry is a file of the archive with the size and SHA-256 it was read with.
type entry struct {
	file *zip.File
	size int64
	sum  string
}

// sourcePattern is a source that can name the report files and the ledger: no path, no colon.
var sourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrBundle is a bundle that cannot be read; the message says where.
type ErrBundle struct{ Message string }

func (e *ErrBundle) Error() string { return "the bundle is not valid: " + e.Message }

func refuse(format string, args ...any) error {
	return &ErrBundle{Message: fmt.Sprintf(format, args...)}
}

// Read reads a bundle archive within the limits: bundle.json and files under files/, no path leaving
// the archive, each file the size and SHA-256 bundle.json gives.
func Read(r io.ReaderAt, size int64) (Archive, error) {
	archive := Archive{files: map[string]entry{}}
	reader, err := zip.NewReader(r, size)
	if err != nil {
		return archive, refuse("not a ZIP archive: %v", err)
	}
	if len(reader.File) > MaxEntries {
		return archive, refuse("%d entries, more than %d", len(reader.File), MaxEntries)
	}
	var total int64
	var manifest []byte
	for _, zf := range reader.File {
		name := zf.Name
		if zf.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(name)
		if clean != name || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.HasPrefix(clean, "..") {
			return archive, refuse("the entry %q leaves the archive", name)
		}
		limit := int64(MaxFileBytes)
		switch {
		case name == "bundle.json":
			limit = MaxJSONBytes
		case strings.HasPrefix(name, "files/"):
		default:
			return archive, refuse("the entry %q is neither bundle.json nor under files/", name)
		}
		if name == "bundle.json" {
			data, err := readEntry(zf, limit)
			if err != nil {
				return archive, refuse("%s: %v", name, err)
			}
			total += int64(len(data))
			manifest = data
		} else {
			size, sum, err := hashEntry(zf, limit)
			if err != nil {
				return archive, refuse("%s: %v", name, err)
			}
			total += size
			archive.files[name] = entry{file: zf, size: size, sum: sum}
		}
		if total > MaxTotalBytes {
			return archive, refuse("more than %d bytes once unpacked", int64(MaxTotalBytes))
		}
	}
	if manifest == nil {
		return archive, refuse("no bundle.json")
	}
	decoder := json.NewDecoder(bytes.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archive.Bundle); err != nil {
		return archive, refuse("bundle.json: %v", err)
	}
	// A series is named in lower case in Colliedesk; a bundle may name it as its codes show it.
	for index := range archive.Bundle.Conversations {
		archive.Bundle.Conversations[index].NumberSeries = strings.ToLower(archive.Bundle.Conversations[index].NumberSeries)
	}
	return archive, archive.check()
}

func readEntry(entry *zip.File, limit int64) ([]byte, error) {
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	body, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return data, nil
}

// hashEntry reads a file of the archive through, within the limit, for its size and SHA-256.
func hashEntry(file *zip.File, limit int64) (int64, string, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return 0, "", fmt.Errorf("larger than %d bytes", limit)
	}
	body, err := file.Open()
	if err != nil {
		return 0, "", err
	}
	defer body.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(body, limit+1))
	if err != nil {
		return 0, "", err
	}
	if size > limit {
		return 0, "", fmt.Errorf("larger than %d bytes", limit)
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

var (
	statuses   = map[string]bool{"open": true, "pending": true, "resolved": true, "closed": true}
	priorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
	kinds      = map[string]bool{"incoming": true, "outgoing": true, "note": true}
)

// check refuses what the bundle says that could not be written as it is.
func (a Archive) check() error {
	b := a.Bundle
	if b.Format != Format {
		return refuse("format %q, want %q", b.Format, Format)
	}
	if len(b.Source) > maxSourceRunes || !sourcePattern.MatchString(b.Source) || strings.Contains(b.Source, "..") {
		return refuse("source must name the bundle in %d letters, digits, dots, dashes or underscores, starting with a letter or digit, without ..", maxSourceRunes)
	}
	contacts := map[string]bool{}
	for index, contact := range b.Contacts {
		if contact.ID == "" || contacts[contact.ID] {
			return refuse("contacts.%d: an id, once", index)
		}
		contacts[contact.ID] = true
		if strings.TrimSpace(contact.Name) == "" && contact.Email == "" {
			return refuse("contacts.%d: a name or an email", index)
		}
		for n, identity := range contact.Identities {
			if identity.ChannelID == "" || identity.Kind == "" || identity.Value == "" {
				return refuse("contacts.%d.identities.%d: channel_id, kind and value", index, n)
			}
		}
	}
	conversations, messages, files := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for index, c := range b.Conversations {
		at := fmt.Sprintf("conversations.%d", index)
		switch {
		case c.ID == "" || conversations[c.ID]:
			return refuse("%s: an id, once", at)
		case !contacts[c.Contact]:
			return refuse("%s: contact %q is not in contacts", at, c.Contact)
		case !statuses[c.Status]:
			return refuse("%s: status %q", at, c.Status)
		case !priorities[c.Priority]:
			return refuse("%s: priority %q", at, c.Priority)
		case c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt):
			return refuse("%s: created_at, and updated_at no earlier", at)
		case (c.Number > 0) != (c.NumberYear > 0):
			return refuse("%s: number and number_year together", at)
		case len(c.Messages) == 0:
			return refuse("%s: no message", at)
		}
		if len(c.Origin) > 0 {
			var origin map[string]json.RawMessage
			if err := json.Unmarshal(c.Origin, &origin); err != nil || origin == nil {
				return refuse("%s.origin: a JSON object", at)
			}
			if _, taken := origin["import"]; taken {
				return refuse("%s.origin.import: Colliedesk records where the conversation came from there", at)
			}
			// The parts the API reads (system, cc, due_at…) must have their types.
			var known domain.ConversationOrigin
			if err := json.Unmarshal(c.Origin, &known); err != nil {
				return refuse("%s.origin: %v", at, err)
			}
		}
		if p := c.Place; p != nil {
			switch {
			case c.ChannelID == "":
				return refuse("%s.place: a place needs the conversation's channel_id", at)
			case p.Kind != "direct" && p.Kind != "group":
				return refuse("%s.place.kind: direct or group", at)
			case p.ExternalID == "" || len([]rune(p.ExternalID)) > 200 || len([]rune(p.Name)) > 200:
				return refuse("%s.place: an external_id, and a name, of at most 200 characters", at)
			}
		}
		conversations[c.ID] = true
		for n, message := range c.Messages {
			where := fmt.Sprintf("%s.messages.%d", at, n)
			if message.ID == "" || messages[message.ID] || !kinds[message.Kind] || message.CreatedAt.IsZero() {
				return refuse("%s: an id once, kind incoming, outgoing or note, created_at", where)
			}
			messages[message.ID] = true
			for f, file := range message.Files {
				stored, ok := a.files[file.Path]
				switch {
				case file.ID == "" || files[file.ID]:
					return refuse("%s.files.%d: an id, once", where, f)
				case !ok:
					return refuse("%s.files.%d: %s is not in the archive", where, f, file.Path)
				case stored.size != file.Size:
					return refuse("%s.files.%d: %s holds %d bytes, not %d", where, f, file.Path, stored.size, file.Size)
				}
				if stored.sum != strings.ToLower(file.SHA256) {
					return refuse("%s.files.%d: %s is not the file whose SHA-256 is %s", where, f, file.Path, file.SHA256)
				}
				files[file.ID] = true
			}
		}
		for n, change := range c.StatusChanges {
			if !statuses[change.From] || !statuses[change.To] || change.From == change.To || change.At.IsZero() {
				return refuse("%s.status_changes.%d: from and to, two statuses, and at", at, n)
			}
		}
	}
	return nil
}

var errNoFile = errors.New("no such file in the bundle")

// open reads a file of the archive again, failing at its end when it is not what was checked.
func (a Archive) open(name string) (io.ReadCloser, int64, error) {
	stored, ok := a.files[name]
	if !ok {
		return nil, 0, errNoFile
	}
	body, err := stored.file.Open()
	if err != nil {
		return nil, 0, err
	}
	return &checked{body: body, hash: sha256.New(), want: stored.sum}, stored.size, nil
}

// checked is a file read back from the archive; its last read fails when the bytes changed since Read.
type checked struct {
	body io.ReadCloser
	hash hash.Hash
	want string
}

func (c *checked) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	c.hash.Write(p[:n])
	if errors.Is(err, io.EOF) && hex.EncodeToString(c.hash.Sum(nil)) != c.want {
		return n, errors.New("the file changed since the bundle was read")
	}
	return n, err
}

func (c *checked) Close() error { return c.body.Close() }
