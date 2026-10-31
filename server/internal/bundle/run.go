package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options say which workspace a bundle goes into. DryRun writes nothing: it checks and reconciles
// everything in a transaction it rolls back, and stores no file.
type Options struct {
	WorkspaceSlug string
	DryRun        bool
	Storage       attachments.Storage
}

// Line is one count of the reconcile: what the source system holds, what the bundle carries, what was
// written.
type Line struct {
	What    string `json:"what"`
	Source  int64  `json:"source"`
	Bundle  int64  `json:"bundle"`
	Written int64  `json:"written"`
}

// Report is the reconcile of an import: OK only when every count agrees, the files read back as they
// were and no reply was queued to go out.
type Report struct {
	Status    string   `json:"status"`
	Source    string   `json:"source"`
	Workspace string   `json:"workspace"`
	DryRun    bool     `json:"dry_run"`
	Lines     []Line   `json:"lines"`
	Problems  []string `json:"problems"`
}

// Statuses of a report.
const (
	StatusOK     = "OK"
	StatusFailed = "FAILED"
)

// ErrTarget lists what the bundle names that the workspace does not have (a channel, a number series, a
// teammate): nothing is written.
type ErrTarget struct{ Missing []string }

func (e *ErrTarget) Error() string {
	return "the workspace has no " + strings.Join(e.Missing, "; no ")
}

// Run imports an archive into a workspace, in one transaction, and reconciles it: a single difference
// rolls everything back and the report says FAILED.
func Run(ctx context.Context, pool *pgxpool.Pool, archive Archive, options Options) (Report, error) {
	db := database.New(pool)
	b := archive.Bundle
	source := "bundle:" + b.Source
	report := Report{Source: source, Workspace: options.WorkspaceSlug, DryRun: options.DryRun, Problems: []string{}}
	workspaceID, err := db.WorkspaceIDBySlug(ctx, options.WorkspaceSlug)
	if err != nil {
		return report, err
	}
	release, err := importkit.AcquireRunLock(ctx, db, workspaceID)
	if err != nil {
		return report, err
	}
	defer release()
	w := &writer{db: db, workspaceID: workspaceID, source: source, archive: archive, dryRun: options.DryRun, storage: options.Storage,
		contacts: map[string]string{}, agents: map[string]string{}, channels: map[string]string{}, objects: map[string]attachments.Object{}, chats: map[string]bool{}}
	if err := w.refuseImported(ctx, db, options.WorkspaceSlug); err != nil {
		return report, err
	}
	// The files are stored before the transaction, so it holds no lock while they are written; what a
	// failure stored goes after the rollback, unless something else of the workspace uses the same bytes.
	var done bool
	defer func() {
		if !done {
			w.forgetStored(context.WithoutCancel(ctx))
		}
	}()
	if err := w.storeFiles(ctx); err != nil {
		return report, err
	}
	tx, err := db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return report, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck
	w.tx = tx
	if err := store.MarkImportWrites(ctx, tx); err != nil {
		return report, err
	}
	if err := w.refuseImported(ctx, nil, options.WorkspaceSlug); err != nil {
		return report, err
	}
	if err := w.targets(ctx); err != nil {
		return report, err
	}
	// Locked until the commit: a cleanup elsewhere waits, then finds the rows; one that ran before finds
	// nothing and the import stops whole.
	if beforeVerify != nil {
		beforeVerify(w.keys())
	}
	if err := store.VerifyObjects(ctx, tx, w.storage, w.keys()...); err != nil {
		return report, err
	}
	if beforeWrite != nil {
		beforeWrite(w.keys())
	}
	if err := w.write(ctx); err != nil {
		return report, err
	}
	report.Lines, report.Problems, err = w.reconcile(ctx)
	if err != nil {
		return report, err
	}
	report.Status = StatusOK
	if len(report.Problems) > 0 {
		report.Status = StatusFailed
		return report, nil
	}
	if options.DryRun {
		return report, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("commit the import: %w", err)
	}
	done = true
	return report, nil
}

type writer struct {
	db          *database.DB
	tx          pgx.Tx
	workspaceID string
	source      string
	archive     Archive
	dryRun      bool
	storage     attachments.Storage
	contacts    map[string]string             // bundle contact id to contact id
	agents      map[string]string             // email to agent id
	channels    map[string]string             // channel id to its type
	objects     map[string]attachments.Object // archive path to the file stored for it
	chats       map[string]bool               // chats recorded in the ledger
}

// refuseImported refuses a source the workspace already has, in the import's transaction (db nil) or
// in a short one of its own before anything is stored.
func (w *writer) refuseImported(ctx context.Context, db *database.DB, slug string) error {
	query := func(tx pgx.Tx) error {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM import_items WHERE workspace_id=$1 AND source=$2)`, w.workspaceID, w.source).Scan(&done); err != nil {
			return err
		}
		if done {
			return fmt.Errorf("%s is already imported into %s: undo it first (collie import undo --workspace %s --source %s)", w.source, slug, slug, w.source)
		}
		return nil
	}
	if db == nil {
		return query(w.tx)
	}
	tx, err := db.BeginWorkspace(ctx, w.workspaceID)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck
	return query(tx)
}

// storeFiles stores each file of the archive the bundle names, once, reading it from the archive again.
// A dry run stores none.
func (w *writer) storeFiles(ctx context.Context) error {
	if w.dryRun {
		return nil
	}
	for _, c := range w.archive.Bundle.Conversations {
		for _, message := range c.Messages {
			for _, file := range message.Files {
				if _, ok := w.objects[file.Path]; ok {
					continue
				}
				body, size, err := w.archive.open(file.Path)
				if err != nil {
					return fmt.Errorf("read %s: %w", file.Path, err)
				}
				object, err := w.storage.Put(ctx, w.workspaceID, body, size)
				body.Close()
				if err != nil {
					return fmt.Errorf("store %s: %w", file.Path, err)
				}
				w.objects[file.Path] = object
			}
		}
	}
	return nil
}

// keys are the stored files, sorted.
func (w *writer) keys() []string {
	keys := make([]string, 0, len(w.objects))
	for _, object := range w.objects {
		keys = append(keys, object.Key)
	}
	sort.Strings(keys)
	return slices.Compact(keys)
}

// targets checks what the bundle names exists in the workspace: its channels, number series and the
// teammates conversations are assigned to.
func (w *writer) targets(ctx context.Context) error {
	var missing []string
	channelIDs, series, assignees := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, contact := range w.archive.Bundle.Contacts {
		for _, identity := range contact.Identities {
			channelIDs[identity.ChannelID] = true
		}
	}
	for _, c := range w.archive.Bundle.Conversations {
		if c.ChannelID != "" {
			channelIDs[c.ChannelID] = true
		}
		if c.NumberSeries != "" {
			series[c.NumberSeries] = true
		}
		if c.AssigneeEmail != "" {
			assignees[strings.ToLower(c.AssigneeEmail)] = true
		}
	}
	for _, id := range sortedKeys(channelIDs) {
		var kind string
		err := w.tx.QueryRow(ctx, `SELECT type FROM channels WHERE workspace_id=$1 AND id::text=$2 AND deleted_at IS NULL`, w.workspaceID, id).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			missing = append(missing, "channel "+id)
			continue
		}
		if err != nil {
			return err
		}
		w.channels[id] = kind
	}
	for _, key := range sortedKeys(series) {
		var exists bool
		if err := w.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_number_series WHERE workspace_id=$1 AND series=$2)`, w.workspaceID, key).Scan(&exists); err != nil {
			return err
		}
		if !exists && key != store.DefaultNumberSeries {
			missing = append(missing, "number series "+key)
		}
	}
	rows, err := w.tx.Query(ctx, `SELECT lower(email),id::text FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL`, w.workspaceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			rows.Close()
			return err
		}
		w.agents[email] = id
	}
	rows.Close()
	for _, email := range sortedKeys(assignees) {
		if w.agents[email] == "" {
			missing = append(missing, "active teammate "+email+" to assign to")
		}
	}
	if len(missing) > 0 {
		return &ErrTarget{Missing: missing}
	}
	return nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (w *writer) mark(ctx context.Context, entity, sourceID, parent, target string, created bool) error {
	meta, _ := json.Marshal(map[string]bool{"created": created})
	_, err := w.tx.Exec(ctx, `INSERT INTO import_items(workspace_id,source,entity,source_id,parent_source_id,target_id,status,meta)
		VALUES($1,$2,$3,$4,NULLIF($5,''),$6::uuid,'done',$7)`, w.workspaceID, w.source, entity, sourceID, parent, target, meta)
	if err != nil {
		return fmt.Errorf("record %s %s: %w", entity, sourceID, err)
	}
	return nil
}

func (w *writer) write(ctx context.Context) error {
	for _, contact := range w.archive.Bundle.Contacts {
		if err := w.contact(ctx, contact); err != nil {
			return err
		}
	}
	for _, c := range w.archive.Bundle.Conversations {
		if err := w.conversation(ctx, c); err != nil {
			return fmt.Errorf("conversation %s: %w", c.ID, err)
		}
	}
	return nil
}

func (w *writer) contact(ctx context.Context, contact Contact) error {
	id, created := "", false
	for _, identity := range contact.Identities {
		err := w.tx.QueryRow(ctx, `SELECT contact_id::text FROM contact_identities WHERE workspace_id=$1 AND channel_id::text=$2 AND kind=$3 AND value=$4`,
			w.workspaceID, identity.ChannelID, identity.Kind, identity.Value).Scan(&id)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if id == "" && contact.Email != "" {
		err := w.tx.QueryRow(ctx, `SELECT id::text FROM contacts WHERE workspace_id=$1 AND lower(email)=lower($2) AND merged_into_id IS NULL ORDER BY created_at LIMIT 1`,
			w.workspaceID, contact.Email).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if id == "" {
		name := strings.TrimSpace(contact.Name)
		if name == "" {
			name = contact.Email
		}
		if err := w.tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email,organization) VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')) RETURNING id::text`,
			w.workspaceID, clip(name), contact.Email, contact.Organization).Scan(&id); err != nil {
			return fmt.Errorf("contact %s: %w", contact.ID, err)
		}
		created = true
	}
	w.contacts[contact.ID] = id
	if err := w.mark(ctx, "contact", contact.ID, "", id, created); err != nil {
		return err
	}
	for _, identity := range contact.Identities {
		var identityID string
		var inserted bool
		if err := w.tx.QueryRow(ctx, `INSERT INTO contact_identities(workspace_id,contact_id,channel_id,kind,value,display_name) VALUES($1,$2,$3::uuid,$4,$5,NULLIF($6,''))
			ON CONFLICT (workspace_id,channel_id,kind,value) DO UPDATE SET updated_at=contact_identities.updated_at RETURNING id::text,(xmax=0)`,
			w.workspaceID, id, identity.ChannelID, identity.Kind, identity.Value, identity.DisplayName).Scan(&identityID, &inserted); err != nil {
			return fmt.Errorf("identity of %s: %w", contact.ID, err)
		}
		if err := w.mark(ctx, "identity", identity.ChannelID+":"+identity.Kind+":"+identity.Value, contact.ID, identityID, inserted); err != nil {
			return err
		}
	}
	return nil
}

func clip(value string) string {
	if runes := []rune(value); len(runes) > maxNameRunes {
		return string(runes[:maxNameRunes])
	}
	return value
}

func (w *writer) conversation(ctx context.Context, c Conversation) error {
	channel, channelID := "imported", any(nil)
	if c.ChannelID != "" {
		channel, channelID = w.channels[c.ChannelID], c.ChannelID
	}
	origin := c.Origin
	if len(origin) == 0 {
		origin = json.RawMessage(`{}`)
	}
	var merged map[string]any
	if err := json.Unmarshal(origin, &merged); err != nil || merged == nil {
		return fmt.Errorf("origin must be a JSON object")
	}
	merged["import"] = w.trace(c.ID)
	// The frame the API reads (ConversationOrigin): where it came from, with empty lists.
	for key, value := range map[string]any{"system": "bundle", "external_id": c.ID, "type": nil, "source": nil, "due_at": nil,
		"first_response_due_at": nil, "cc": []string{}, "forwarded_to": []string{}} {
		if _, ok := merged[key]; !ok {
			merged[key] = value
		}
	}
	origin, _ = json.Marshal(merged)
	series := c.NumberSeries
	if series == "" {
		series = store.DefaultNumberSeries
	}
	var number, numberYear any
	if c.Number > 0 {
		number, numberYear = c.Number, c.NumberYear
	}
	var assignee any
	if c.AssigneeEmail != "" {
		assignee = w.agents[strings.ToLower(c.AssigneeEmail)]
	}
	subject := strings.TrimSpace(c.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	var id string
	if err := w.tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,priority,assignee_id,created_at,updated_at,origin,channel,channel_id,
			number_series,number,number_year,sla_applies)
		VALUES($1,$2,$3,$4,$5,$6::uuid,$7,$8,$9,$10,$11::uuid,$12,$13,$14,$15) RETURNING id::text`,
		w.workspaceID, w.contacts[c.Contact], subject, c.Status, c.Priority, assignee, c.CreatedAt.UTC(), c.UpdatedAt.UTC(), origin, channel, channelID,
		series, number, numberYear, c.SLAApplies).Scan(&id); err != nil {
		return err
	}
	if err := w.mark(ctx, "ticket", c.ID, "", id, true); err != nil {
		return err
	}
	for _, name := range c.Tags {
		var tagID string
		var inserted bool
		if err := w.tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2) ON CONFLICT (workspace_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id::text,(xmax=0)`,
			w.workspaceID, name).Scan(&tagID, &inserted); err != nil {
			return err
		}
		if inserted {
			if err := w.mark(ctx, "tag", name, "", tagID, true); err != nil {
				return err
			}
		}
		if _, err := w.tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, w.workspaceID, id, tagID); err != nil {
			return err
		}
	}
	for _, key := range c.ThreadKeys {
		if _, err := w.tx.Exec(ctx, `INSERT INTO conversation_thread_keys(workspace_id,conversation_id,channel_type,key,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			w.workspaceID, id, channel, key, c.CreatedAt.UTC()); err != nil {
			return fmt.Errorf("thread key: %w", err)
		}
	}
	for _, change := range c.StatusChanges {
		if _, err := w.tx.Exec(ctx, `INSERT INTO conversation_status_changes(workspace_id,conversation_id,from_status,to_status,changed_at,source) VALUES($1,$2,$3,$4,$5,'import')`,
			w.workspaceID, id, change.From, change.To, change.At.UTC()); err != nil {
			return fmt.Errorf("status change: %w", err)
		}
	}
	thread, err := w.chat(ctx, c)
	if err != nil {
		return err
	}
	for _, message := range c.Messages {
		messageID, err := w.message(ctx, c, id, message)
		if err != nil {
			return fmt.Errorf("message %s: %w", message.ID, err)
		}
		if err := w.chatItem(ctx, c, thread, message, messageID); err != nil {
			return fmt.Errorf("message %s in its chat: %w", message.ID, err)
		}
	}
	return nil
}

// chat is the chat of the channel the conversation was opened in (its place), the one the channel has or a
// new one; "" without a place.
func (w *writer) chat(ctx context.Context, c Conversation) (string, error) {
	if c.Place == nil {
		return "", nil
	}
	var contact any
	if c.Place.Kind == "direct" {
		contact = w.contacts[c.Contact]
	}
	var id string
	var created bool
	if err := w.tx.QueryRow(ctx, `INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,name,contact_id,opens_conversations)
		VALUES($1,$2::uuid,$3,$4,NULLIF($5,''),$6::uuid,true)
		ON CONFLICT (workspace_id,channel_id,external_id) DO UPDATE SET updated_at=channel_threads.updated_at RETURNING id::text,(xmax=0)`,
		w.workspaceID, c.ChannelID, c.Place.Kind, c.Place.ExternalID, c.Place.Name, contact).Scan(&id, &created); err != nil {
		return "", fmt.Errorf("chat %s: %w", c.Place.ExternalID, err)
	}
	if !w.chats[id] {
		w.chats[id] = true
		if err := w.mark(ctx, "chat", c.ChannelID+":"+c.Place.ExternalID, "", id, created); err != nil {
			return "", err
		}
	}
	return id, nil
}

// chatItem shows a customer's message or a reply in the conversation's chat; a note is no part of it.
func (w *writer) chatItem(ctx context.Context, c Conversation, thread string, message Message, messageID string) error {
	if thread == "" || message.Kind == "note" {
		return nil
	}
	direction, role, contact, agent, sender := "in", "customer", any(w.contacts[c.Contact]), any(nil), contactName(w.archive.Bundle, c.Contact)
	identity := contactIdentity(w.archive.Bundle, c.Contact, c.ChannelID)
	if s := message.Sender; s != nil && message.Kind == "incoming" && (s.Kind != identity.Kind || s.Value != identity.Value) {
		// Another member of the chat: named as they wrote, no contact of the conversation.
		identity, sender, contact = Identity{Kind: s.Kind, Value: s.Value}, s.Name, nil
	}
	if message.Kind == "outgoing" {
		direction, role, contact, sender, identity = "out", "staff", nil, message.AuthorName, Identity{}
		if id := w.agents[strings.ToLower(message.AuthorEmail)]; message.AuthorEmail != "" && id != "" {
			agent = id
		}
	}
	var id string
	// Named apart from the channel's own message ids (and another bundle's): bundle:<source>:<id>.
	if err := w.tx.QueryRow(ctx, `INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,sender_kind,sender_value,sender_name,contact_id,agent_id,
			external_id,body,message_id,disposition,occurred_at,created_at)
		VALUES($1,$2::uuid,$3::uuid,$4,$5,NULLIF(left($13,40),''),NULLIF(left($14,200),''),NULLIF(left($6,200),''),$7::uuid,$8::uuid,left($9,1000),left($10,20000),$11::uuid,
			'conversation',$12,$12) RETURNING id::text`,
		w.workspaceID, c.ChannelID, thread, direction, role, sender, contact, agent, "bundle:"+w.archive.Bundle.Source+":"+message.ID, message.Body, messageID,
		message.CreatedAt.UTC(), identity.Kind, identity.Value).Scan(&id); err != nil {
		return err
	}
	// The conversation is in this chat: its replies go there.
	if _, err := w.tx.Exec(ctx, `INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason,linked_at) SELECT $1,m.conversation_id,$2::uuid,'import',$3
		FROM messages m WHERE m.workspace_id=$1 AND m.id=$4::uuid`, w.workspaceID, id, message.CreatedAt.UTC(), messageID); err != nil {
		return err
	}
	if identity.Value != "" {
		if err := store.RecordImportedSender(ctx, w.tx, w.workspaceID, c.ChannelID, thread, role,
			channels.Sender{Identity: channels.Identity{Kind: identity.Kind, Value: identity.Value}, Name: sender}, message.CreatedAt.UTC()); err != nil {
			return err
		}
	}
	if _, err := w.tx.Exec(ctx, `UPDATE channel_threads SET last_item_at=GREATEST(COALESCE(last_item_at,$3),$3) WHERE workspace_id=$1 AND id=$2::uuid`,
		w.workspaceID, thread, message.CreatedAt.UTC()); err != nil {
		return err
	}
	return w.mark(ctx, "chat_item", message.ID, c.ID, id, true)
}

// contactIdentity is how the channel knows the conversation's customer, empty when the bundle gives none.
func contactIdentity(b Bundle, id, channelID string) Identity {
	for _, contact := range b.Contacts {
		if contact.ID == id {
			for _, identity := range contact.Identities {
				if identity.ChannelID == channelID {
					return identity
				}
			}
		}
	}
	return Identity{}
}

func contactName(b Bundle, id string) string {
	for _, contact := range b.Contacts {
		if contact.ID == id {
			if strings.TrimSpace(contact.Name) != "" {
				return contact.Name
			}
			return contact.Email
		}
	}
	return ""
}

// trace is where an imported row came from: origin.import.
func (w *writer) trace(id string) map[string]string {
	return map[string]string{"source": "bundle", "bundle": w.archive.Bundle.Source, "id": id}
}

func (w *writer) message(ctx context.Context, c Conversation, conversationID string, message Message) (string, error) {
	var author any
	if message.AuthorEmail != "" && message.Kind != "incoming" {
		if id := w.agents[strings.ToLower(message.AuthorEmail)]; id != "" {
			author = id
		}
	}
	kind := map[string]string{"incoming": "incoming", "outgoing": "reply", "note": "private_note"}[message.Kind]
	origin, _ := json.Marshal(map[string]any{"import": w.trace(message.ID), "system": "bundle", "external_id": message.ID, "kind": kind,
		"from": nil, "to": []string{}, "cc": []string{}, "bcc": []string{}})
	var id string
	if err := w.tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,author_name,kind,body,origin,created_at)
		VALUES($1,$2,$3::uuid,NULLIF($4,''),$5,$6,$7,$8) RETURNING id::text`,
		w.workspaceID, conversationID, author, clip(message.AuthorName), message.Kind, message.Body, origin, message.CreatedAt.UTC()).Scan(&id); err != nil {
		return "", err
	}
	if err := w.mark(ctx, "thread", message.ID, c.ID, id, true); err != nil {
		return "", err
	}
	for _, file := range message.Files {
		if w.dryRun {
			continue
		}
		object, ok := w.objects[file.Path]
		if !ok {
			return "", fmt.Errorf("%s: %w", file.Path, errNoFile)
		}
		name := strings.TrimSpace(file.Name)
		if name == "" {
			name = "file-" + file.ID
		}
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		attachment, err := store.InsertAttachment(ctx, w.tx, w.workspaceID, store.NewAttachment{MessageID: id, Filename: name, ContentType: contentType, Object: object})
		if err != nil {
			return "", err
		}
		if err := w.mark(ctx, "attachment", file.ID, c.ID, attachment.ID, true); err != nil {
			return "", err
		}
	}
	return id, nil
}

// forgetStored removes, after the rollback, the files a failed import stored that nothing else of the
// workspace references (the store keeps one file for the same bytes).
func (w *writer) forgetStored(ctx context.Context) {
	for _, key := range w.keys() {
		_ = store.DiscardObject(ctx, w.db, w.storage, w.workspaceID, key)
	}
}

// reconcile counts what the transaction holds against the bundle and the source system's counts, reads
// every stored file back, and finds no reply queued to go out.
func (w *writer) reconcile(ctx context.Context) ([]Line, []string, error) {
	b := w.archive.Bundle
	var carried Counts
	carried.Contacts, carried.Conversations = len(b.Contacts), len(b.Conversations)
	for _, contact := range b.Contacts {
		carried.Identities += len(contact.Identities)
	}
	for _, c := range b.Conversations {
		carried.StatusChanges += len(c.StatusChanges)
		carried.ThreadKeys += len(c.ThreadKeys)
		for _, message := range c.Messages {
			switch message.Kind {
			case "incoming":
				carried.Incoming++
			case "outgoing":
				carried.Outgoing++
			default:
				carried.Notes++
			}
			for _, file := range message.Files {
				carried.Files++
				carried.FileBytes += file.Size
			}
		}
	}
	var written Counts
	if err := w.tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM import_items WHERE workspace_id=$1 AND source=$2 AND entity='contact'),
			(SELECT count(*) FROM import_items WHERE workspace_id=$1 AND source=$2 AND entity='identity'),
			(SELECT count(*) FROM import_items i JOIN conversations c ON c.workspace_id=i.workspace_id AND c.id=i.target_id WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='ticket'),
			(SELECT count(*) FILTER (WHERE m.kind='incoming') FROM import_items i JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.target_id WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='thread'),
			(SELECT count(*) FILTER (WHERE m.kind='outgoing') FROM import_items i JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.target_id WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='thread'),
			(SELECT count(*) FILTER (WHERE m.kind='note') FROM import_items i JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.target_id WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='thread'),
			(SELECT count(*) FROM conversation_status_changes h JOIN import_items i ON i.workspace_id=h.workspace_id AND i.target_id=h.conversation_id AND i.entity='ticket' AND i.source=$2
				WHERE h.workspace_id=$1 AND h.source='import'),
			(SELECT count(*) FROM conversation_thread_keys k JOIN import_items i ON i.workspace_id=k.workspace_id AND i.target_id=k.conversation_id AND i.entity='ticket' AND i.source=$2
				WHERE k.workspace_id=$1)`, w.workspaceID, w.source).Scan(&written.Contacts, &written.Identities, &written.Conversations,
		&written.Incoming, &written.Outgoing, &written.Notes, &written.StatusChanges, &written.ThreadKeys); err != nil {
		return nil, nil, fmt.Errorf("count what was written: %w", err)
	}
	var problems []string
	if w.dryRun {
		// No file is stored in a dry run: what counts is that every one was read and checked.
		written.Files, written.FileBytes = carried.Files, carried.FileBytes
	} else {
		rows, err := w.tx.Query(ctx, `SELECT a.storage_key,a.size,a.sha256 FROM attachments a JOIN import_items i ON i.workspace_id=a.workspace_id AND i.target_id=a.id
			WHERE a.workspace_id=$1 AND i.source=$2 AND i.entity='attachment'`, w.workspaceID, w.source)
		if err != nil {
			return nil, nil, err
		}
		type stored struct {
			key, sum string
			size     int64
		}
		var files []stored
		for rows.Next() {
			var item stored
			if err := rows.Scan(&item.key, &item.size, &item.sum); err != nil {
				rows.Close()
				return nil, nil, err
			}
			files = append(files, item)
		}
		rows.Close()
		for _, file := range files {
			written.Files++
			written.FileBytes += file.size
			if problem := readBack(ctx, w.storage, file.key, file.sum); problem != "" {
				problems = append(problems, problem)
			}
		}
	}
	var queued int
	if err := w.tx.QueryRow(ctx, `SELECT count(*) FROM channel_outbox o JOIN import_items i ON i.workspace_id=o.workspace_id AND i.entity='thread' AND i.source=$2 AND i.target_id=o.message_id
		WHERE o.workspace_id=$1`, w.workspaceID, w.source).Scan(&queued); err != nil {
		return nil, nil, err
	}
	if queued > 0 {
		problems = append(problems, fmt.Sprintf("%d imported messages were queued to be sent", queued))
	}
	lines := []Line{
		{"contacts", int64(b.Counts.Contacts), int64(carried.Contacts), int64(written.Contacts)},
		{"identities", int64(b.Counts.Identities), int64(carried.Identities), int64(written.Identities)},
		{"conversations", int64(b.Counts.Conversations), int64(carried.Conversations), int64(written.Conversations)},
		{"incoming messages", int64(b.Counts.Incoming), int64(carried.Incoming), int64(written.Incoming)},
		{"replies", int64(b.Counts.Outgoing), int64(carried.Outgoing), int64(written.Outgoing)},
		{"notes", int64(b.Counts.Notes), int64(carried.Notes), int64(written.Notes)},
		{"status changes", int64(b.Counts.StatusChanges), int64(carried.StatusChanges), int64(written.StatusChanges)},
		{"thread keys", int64(b.Counts.ThreadKeys), int64(carried.ThreadKeys), int64(written.ThreadKeys)},
		{"files", int64(b.Counts.Files), int64(carried.Files), int64(written.Files)},
		{"file bytes", b.Counts.FileBytes, carried.FileBytes, written.FileBytes},
	}
	for _, line := range lines {
		if line.Source != line.Bundle || line.Bundle != line.Written {
			problems = append(problems, fmt.Sprintf("%s: the source holds %d, the bundle carries %d, %d were written", line.What, line.Source, line.Bundle, line.Written))
		}
	}
	return lines, problems, nil
}

func readBack(ctx context.Context, storage attachments.Storage, key, sum string) string {
	body, err := storage.Open(ctx, key)
	if err != nil {
		return "a stored file cannot be read back: " + err.Error()
	}
	defer body.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, body); err != nil {
		return "a stored file cannot be read back: " + err.Error()
	}
	if hex.EncodeToString(hash.Sum(nil)) != sum {
		return "a stored file does not read back as it was written"
	}
	return ""
}

// WriteFiles writes the report as JSON and Markdown into dir, and returns their paths.
func (r Report) WriteFiles(dir string) (string, string, error) {
	if dir == "" {
		dir = "."
	}
	base := filepath.Join(dir, filepath.Base("bundle-import-"+strings.TrimPrefix(r.Source, "bundle:")))
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(base+".json", data, 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(base+".md", []byte(r.Markdown()), 0o600); err != nil {
		return "", "", err
	}
	return base + ".json", base + ".md", nil
}

// Markdown is the report as a person reads it.
func (r Report) Markdown() string {
	var b strings.Builder
	mode := ""
	if r.DryRun {
		mode = " (dry run: nothing was written)"
	}
	fmt.Fprintf(&b, "# Bundle import: %s%s\n\n- Source: %s\n- Workspace: %s\n\n| | Source | Bundle | Written |\n|---|---:|---:|---:|\n", r.Status, mode, r.Source, r.Workspace)
	for _, line := range r.Lines {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", line.What, line.Source, line.Bundle, line.Written)
	}
	if len(r.Problems) > 0 {
		b.WriteString("\nNothing was kept. What did not agree:\n\n")
		for _, problem := range r.Problems {
			fmt.Fprintf(&b, "- %s\n", problem)
		}
	}
	return b.String()
}

// Test hooks (export_test.go).
var beforeVerify, beforeWrite func(keys []string)
