package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// MentionRefusedError names the first mention of a note that is not an active member of the workspace:
// its place in the list and what was given. Nothing of the note is stored.
type MentionRefusedError struct {
	Index int
	Given string
}

func (e *MentionRefusedError) Error() string {
	return fmt.Sprintf("mention %d (%q) is not an active member of this workspace", e.Index, e.Given)
}

var (
	// ErrMentionsOnlyOnNotes means mentions came with a reply or a customer message: only a note asks teammates.
	ErrMentionsOnlyOnNotes = errors.New("only a private note can mention teammates")
	// ErrNotYourNote means someone other than its writer tried to add people to a note.
	ErrNotYourNote = errors.New("only the person who wrote the note can add people to it")
)

// excerptLength is how many characters of a note the Mentions list and the email quote.
const excerptLength = 200

// mention asks the teammates refs names (agent ids or email addresses) on the note messageID: each active
// member of the workspace is asked once, whoever was asked before stays as they were, and the writer is
// never asked. A ref that is not an active member refuses the whole note.
func mention(ctx context.Context, tx pgx.Tx, workspaceID, messageID, writerID string, refs []string) error {
	asked := make([]string, 0, len(refs))
	for index, ref := range refs {
		var id string
		err := tx.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL
			AND (id::text=lower($2) OR lower(email)=lower($2))`, workspaceID, strings.TrimSpace(ref)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return &MentionRefusedError{Index: index, Given: ref}
		}
		if err != nil {
			return fmt.Errorf("find mentioned teammate: %w", err)
		}
		if id != writerID {
			asked = append(asked, id)
		}
	}
	for _, id := range asked {
		// One statement each, so clock_timestamp keeps the order they were asked in.
		if _, err := tx.Exec(ctx, `INSERT INTO note_mentions(workspace_id,message_id,agent_id,created_at,email)
			SELECT $1,$2,a.id,clock_timestamp(),CASE WHEN a.mention_email THEN 'queued' ELSE 'off' END
			FROM agents a WHERE a.workspace_id=$1 AND a.id=$3 AND a.deactivated_at IS NULL
			ON CONFLICT (workspace_id,message_id,agent_id) DO NOTHING`, workspaceID, messageID, id); err != nil {
			return fmt.Errorf("store mention: %w", err)
		}
	}
	return nil
}

// attachMentions fills the Mentions of the notes among messages.
func attachMentions(ctx context.Context, q rowsQuerier, workspaceID string, messages []domain.Message) error {
	ids := make([]string, 0, len(messages))
	at := map[string]int{}
	for index, message := range messages {
		if message.Kind == "note" {
			ids = append(ids, message.ID)
			at[message.ID] = index
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT n.message_id::text,n.agent_id::text,COALESCE(NULLIF(a.display_name,''),a.email),n.email<>'off',n.read_at
		FROM note_mentions n JOIN agents a ON a.workspace_id=n.workspace_id AND a.id=n.agent_id
		WHERE n.workspace_id=$1 AND n.message_id=ANY($2::uuid[]) ORDER BY n.created_at,n.id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read mentions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var item domain.NoteMention
		if err := rows.Scan(&messageID, &item.AgentID, &item.Name, &item.ByEmail, &item.ReadAt); err != nil {
			return fmt.Errorf("read mentions: %w", err)
		}
		item.ReadAt = domain.UTCOptional(item.ReadAt)
		messages[at[messageID]].Mentions = append(messages[at[messageID]].Mentions, item)
	}
	return rows.Err()
}

// AddMentions asks more teammates on a note writerID wrote: only those it did not ask yet are told.
func (s *Store) AddMentions(ctx context.Context, workspaceID, conversationID, messageID, writerID string, refs []string) (domain.Message, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Message, error) {
		var kind, author string
		if err := tx.QueryRow(ctx, `SELECT kind,COALESCE(author_agent_id::text,'') FROM messages
			WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`, workspaceID, conversationID, messageID).Scan(&kind, &author); err != nil {
			return domain.Message{}, fmt.Errorf("find note: %w", err)
		}
		if kind != "note" {
			return domain.Message{}, ErrMentionsOnlyOnNotes
		}
		if author != writerID {
			return domain.Message{}, ErrNotYourNote
		}
		if err := mention(ctx, tx, workspaceID, messageID, writerID, refs); err != nil {
			return domain.Message{}, err
		}
		return getMessage(ctx, tx, workspaceID, messageID)
	})
}

// MentionPage is one page of a person's Mentions list.
type MentionPage struct {
	Items      []domain.Mention `json:"items"`
	Unread     int              `json:"unread"`
	Email      bool             `json:"email"`
	NextBefore *string          `json:"next_before"`
}

// ListMentions lists the notes that ask agentID, newest first: only the unread ones unless all, limit at a
// time, after the mention before (empty: from the newest).
func (s *Store) ListMentions(ctx context.Context, workspaceID, agentID string, all bool, limit int, before string) (MentionPage, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MentionPage, error) {
		page := MentionPage{Items: []domain.Mention{}}
		rows, err := tx.Query(ctx, `SELECT n.id::text,m.conversation_id::text,n.message_id::text,c.subject,COALESCE(c.display_ref,''),m.body,
				m.author_agent_id::text,COALESCE(NULLIF(aa.display_name,''),aa.email),t.id::text,t.label,COALESCE(NULLIF(ta.display_name,''),ta.email),n.created_at,n.read_at
			FROM note_mentions n JOIN messages m ON m.workspace_id=n.workspace_id AND m.id=n.message_id
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			LEFT JOIN agents aa ON aa.workspace_id=m.workspace_id AND aa.id=m.author_agent_id
			LEFT JOIN sessions t ON t.workspace_id=m.workspace_id AND t.id=m.agent_token_id
			LEFT JOIN agents ta ON ta.workspace_id=t.workspace_id AND ta.id=t.agent_id
			WHERE n.workspace_id=$1 AND n.agent_id=$2 AND ($3 OR n.read_at IS NULL)
				AND (NULLIF($4,'') IS NULL OR (n.created_at,n.id)<(SELECT b.created_at,b.id FROM note_mentions b
					WHERE b.workspace_id=$1 AND b.agent_id=$2 AND b.id::text=$4))
			ORDER BY n.created_at DESC,n.id DESC LIMIT $5`, workspaceID, agentID, all, before, limit+1)
		if err != nil {
			return page, fmt.Errorf("list mentions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item domain.Mention
			var body string
			var tokenID, tokenLabel, tokenAgent *string
			if err := rows.Scan(&item.ID, &item.ConversationID, &item.MessageID, &item.Subject, &item.DisplayRef, &body,
				&item.AuthorAgentID, &item.AuthorName, &tokenID, &tokenLabel, &tokenAgent, &item.CreatedAt, &item.ReadAt); err != nil {
				return page, fmt.Errorf("list mentions: %w", err)
			}
			if tokenID != nil && tokenLabel != nil && tokenAgent != nil {
				item.AgentToken = &domain.MessageToken{ID: *tokenID, Label: *tokenLabel, AgentName: *tokenAgent}
			}
			item.Excerpt = Excerpt(body)
			item.CreatedAt, item.ReadAt = domain.UTC(item.CreatedAt), domain.UTCOptional(item.ReadAt)
			page.Items = append(page.Items, item)
		}
		if err := rows.Err(); err != nil {
			return page, err
		}
		rows.Close()
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			next := page.Items[limit-1].ID
			page.NextBefore = &next
		}
		err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM note_mentions WHERE workspace_id=$1 AND agent_id=$2 AND read_at IS NULL),
			COALESCE((SELECT mention_email FROM agents WHERE workspace_id=$1 AND id=$2),false)`, workspaceID, agentID).Scan(&page.Unread, &page.Email)
		return page, err
	})
}

// Excerpt is the start of a note on one line, at most excerptLength characters.
func Excerpt(body string) string {
	text := strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(text) <= excerptLength {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:excerptLength-1])) + "…"
}

// MentionSelection names the mentions of one person to mark: some by id, those on a conversation's notes,
// or all of them. Exactly one is set.
type MentionSelection struct {
	IDs            []string
	ConversationID string
	All            bool
}

// MarkMentions marks agentID's selected mentions read, or unread again, and says how many changed and how
// many stay unread. An id that is not one of theirs, or a conversation of another workspace, is
// pgx.ErrNoRows and nothing changes.
func (s *Store) MarkMentions(ctx context.Context, workspaceID, agentID string, selection MentionSelection, read bool) (changed, unread int, err error) {
	type counts struct{ changed, unread int }
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (counts, error) {
		var found bool
		switch {
		case len(selection.IDs) > 0:
			err := tx.QueryRow(ctx, `SELECT count(*)=cardinality($3::uuid[]) FROM note_mentions WHERE workspace_id=$1 AND agent_id=$2 AND id=ANY($3::uuid[])`,
				workspaceID, agentID, selection.IDs).Scan(&found)
			if err != nil {
				return counts{}, fmt.Errorf("find mentions: %w", err)
			}
		case selection.ConversationID != "":
			err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id::text=$2)`, workspaceID, selection.ConversationID).Scan(&found)
			if err != nil {
				return counts{}, fmt.Errorf("find conversation: %w", err)
			}
		default:
			found = selection.All
		}
		if !found {
			return counts{}, fmt.Errorf("mentions: %w", pgx.ErrNoRows)
		}
		tag, err := tx.Exec(ctx, `UPDATE note_mentions SET read_at=CASE WHEN $3 THEN now() END
			WHERE workspace_id=$1 AND agent_id=$2 AND (read_at IS NULL)=$3
				AND ($4::uuid[] IS NULL OR id=ANY($4::uuid[]))
				AND (NULLIF($5,'') IS NULL OR message_id IN (SELECT id FROM messages WHERE workspace_id=$1 AND conversation_id::text=$5))`,
			workspaceID, agentID, read, nilIfEmpty(selection.IDs), selection.ConversationID)
		if err != nil {
			return counts{}, fmt.Errorf("mark mentions: %w", err)
		}
		var result counts
		result.changed = int(tag.RowsAffected())
		err = tx.QueryRow(ctx, `SELECT count(*) FROM note_mentions WHERE workspace_id=$1 AND agent_id=$2 AND read_at IS NULL`, workspaceID, agentID).Scan(&result.unread)
		return result, err
	})
	return result.changed, result.unread, err
}

func nilIfEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// SetMentionEmail turns a teammate's own mention emails on or off; mentions already queued keep going.
func (s *Store) SetMentionEmail(ctx context.Context, workspaceID, agentID string, on bool) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE agents SET mention_email=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, on)
		return struct{}{}, err
	})
	return err
}

// MentionEmailDue is a mention whose email waits to go: queued, or left sending by a stopped server.
type MentionEmailDue struct{ WorkspaceID, ID string }

// DueMentionEmails lists, across workspaces, the mention emails to send now, oldest first.
func (s *Store) DueMentionEmails(ctx context.Context, abandoned time.Duration, limit int) ([]MentionEmailDue, error) {
	var due []MentionEmailDue
	err := s.db.Privileged(ctx, database.PrivilegeMentionEmailClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,id::text FROM note_mentions
			WHERE email='queued' OR (email='sending' AND email_claimed_at<now()-make_interval(secs => $1))
			ORDER BY created_at LIMIT $2`, abandoned.Seconds(), limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (MentionEmailDue, error) {
			var item MentionEmailDue
			return item, row.Scan(&item.WorkspaceID, &item.ID)
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list due mention emails: %w", err)
	}
	return due, nil
}

// MentionEmail is what one mention email says, and to whom.
type MentionEmail struct {
	MentionEmailDue
	To, Asker, Token             string
	Slug, WorkspaceName          string
	ConversationID, MessageID    string
	Subject, DisplayRef, Excerpt string
}

// ClaimMentionEmail claims one mention's email right before it goes, unless another server is sending it or
// it went. A person deactivated, or who turned these emails off, since the note was posted gets none.
func (s *Store) ClaimMentionEmail(ctx context.Context, due MentionEmailDue, abandoned time.Duration) (MentionEmail, bool, error) {
	mail := MentionEmail{MentionEmailDue: due}
	claimed, err := scoped(ctx, s, due.WorkspaceID, func(tx pgx.Tx) (bool, error) {
		var active, wanted bool
		var body string
		err := tx.QueryRow(ctx, `UPDATE note_mentions n SET email='sending',email_claimed_at=now()
			FROM agents p, messages m, conversations c, workspaces w
			WHERE n.workspace_id=$1 AND n.id=$2 AND (n.email='queued' OR (n.email='sending' AND n.email_claimed_at<now()-make_interval(secs => $3)))
				AND p.workspace_id=n.workspace_id AND p.id=n.agent_id AND m.workspace_id=n.workspace_id AND m.id=n.message_id
				AND c.workspace_id=m.workspace_id AND c.id=m.conversation_id AND w.id=n.workspace_id
			RETURNING p.email,p.deactivated_at IS NULL,p.mention_email,w.slug,w.name,c.id::text,m.id::text,c.subject,COALESCE(c.display_ref,''),m.body,
				COALESCE((SELECT COALESCE(NULLIF(a.display_name,''),a.email) FROM agents a WHERE a.workspace_id=m.workspace_id AND a.id=m.author_agent_id),'A teammate'),
				COALESCE((SELECT t.label FROM sessions t WHERE t.workspace_id=m.workspace_id AND t.id=m.agent_token_id),'')`,
			due.WorkspaceID, due.ID, abandoned.Seconds()).Scan(&mail.To, &active, &wanted, &mail.Slug, &mail.WorkspaceName, &mail.ConversationID, &mail.MessageID,
			&mail.Subject, &mail.DisplayRef, &body, &mail.Asker, &mail.Token)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("claim mention email: %w", err)
		}
		mail.Excerpt = Excerpt(body)
		if !active || !wanted {
			_, err := tx.Exec(ctx, `UPDATE note_mentions SET email='off' WHERE workspace_id=$1 AND id=$2`, due.WorkspaceID, due.ID)
			return false, err
		}
		return true, nil
	})
	return mail, claimed, err
}

// SettleMentionEmail records how a mention's email ended: sent or rejected; "" gives the claim back for a
// later round (a send that failed for now).
func (s *Store) SettleMentionEmail(ctx context.Context, due MentionEmailDue, status string) error {
	if status == "" {
		status = "queued"
	}
	_, err := scoped(ctx, s, due.WorkspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE note_mentions SET email=$3 WHERE workspace_id=$1 AND id=$2 AND email='sending'`, due.WorkspaceID, due.ID, status)
		return struct{}{}, err
	})
	return err
}
