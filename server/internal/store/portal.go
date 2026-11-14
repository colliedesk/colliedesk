package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

const (
	// PortalLinkLifetime is how long a sign-in link works. It is spent by the Continue button, once.
	PortalLinkLifetime = 15 * time.Minute
	// PortalSessionLifetime is how long a portal sign-in lasts. Signing out ends it sooner.
	PortalSessionLifetime = 30 * 24 * time.Hour
)

// ErrPortalTicketNotFound means the number is not a request this address can see. Callers answer it
// the same way as a page that does not exist, so a guess learns nothing.
var ErrPortalTicketNotFound = errors.New("portal ticket not found")

// portalVisible is the only rule for what a signed-in address may see. The contact's own email sees
// every request on that contact. An address merged into them sees the requests the merge moved and
// the ones they open afterwards (via portal, which only this server sets, from the proven address),
// never the kept address's other requests. A private note is not a message this query reads.
const portalVisible = `c.workspace_id=$1 AND c.merged_into_id IS NULL AND c.status<>'spam' AND EXISTS (
	SELECT 1 FROM contacts k WHERE k.workspace_id=c.workspace_id AND k.id=c.contact_id AND k.merged_into_id IS NULL AND (
		lower(k.email)=lower($2)
		OR EXISTS (SELECT 1 FROM messages own WHERE own.workspace_id=c.workspace_id AND own.conversation_id=c.id
			AND own.kind='incoming' AND own.email_meta->>'via'='portal' AND lower(own.email_meta->>'from')=lower($2))
		OR EXISTS (SELECT 1 FROM contact_merges g JOIN contacts d ON d.workspace_id=g.workspace_id AND d.id=g.merged_contact_id
			WHERE g.workspace_id=c.workspace_id AND g.undone_at IS NULL AND lower(d.email)=lower($2) AND c.id=ANY(g.conversation_ids))
	))`

// PortalTicketSummary is one request as the list shows it.
type PortalTicketSummary struct {
	Number       int64
	Series       string
	Ref, Subject string
	Status       string
	Updated      time.Time
}

// PortalMessage is one public line of a request. Notes are never read into it.
type PortalMessage struct {
	Kind, Body, Author string
	At                 time.Time
}

// PortalTicket is one request and its public conversation.
type PortalTicket struct {
	PortalTicketSummary
	Messages []PortalMessage
	// More is true when older public messages were left off the page.
	More bool
}

// PortalLink is the result of spending a sign-in link.
type PortalLink struct {
	// Status is ok, used, expired or unknown. Only ok carries a session.
	Status  string
	Session string
	Email   string
}

func portalToken() (string, []byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return raw, portalHash(raw), nil
}

func portalHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreatePortalLink stores a one-time sign-in link for email and returns the token, shown once in the
// email. The row keeps only its hash. Expired links of the workspace are deleted as this runs.
func (s *Store) CreatePortalLink(ctx context.Context, workspaceID, email string) (string, error) {
	raw, hash, err := portalToken()
	if err != nil {
		return "", err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	now := s.now()
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM portal_links WHERE id IN (SELECT id FROM portal_links
			WHERE workspace_id=$1 AND expires_at<=$2 LIMIT 100)`, workspaceID, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO portal_links(workspace_id,email,token_hash,expires_at) VALUES($1,$2,$3,$4)`,
			workspaceID, email, hash, now.Add(PortalLinkLifetime))
		return err
	})
	if err != nil {
		return "", fmt.Errorf("create portal link: %w", err)
	}
	return raw, nil
}

// ForgetPortalLink deletes an unused link, when the email that would have carried it was not sent.
func (s *Store) ForgetPortalLink(ctx context.Context, workspaceID, token string) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM portal_links WHERE workspace_id=$1 AND token_hash=$2 AND used_at IS NULL`, workspaceID, portalHash(token))
		return err
	})
	if err != nil {
		return fmt.Errorf("forget portal link: %w", err)
	}
	return nil
}

// PortalLinkStatus reports a link without spending it: ready, used, expired or unknown.
func (s *Store) PortalLinkStatus(ctx context.Context, workspaceID, token string) (string, error) {
	var status string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var expires time.Time
		var used *time.Time
		err := tx.QueryRow(ctx, `SELECT expires_at,used_at FROM portal_links WHERE workspace_id=$1 AND token_hash=$2`,
			workspaceID, portalHash(token)).Scan(&expires, &used)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			status = "unknown"
			return nil
		case err != nil:
			return err
		case used != nil:
			status = "used"
		case !expires.After(s.now()):
			status = "expired"
		default:
			status = "ready"
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read portal link: %w", err)
	}
	return status, nil
}

// RedeemPortalLink spends a link and opens a session. A link that is missing, already spent or past
// its time opens nothing; Status says which, and each of those is safe to show the requester.
func (s *Store) RedeemPortalLink(ctx context.Context, workspaceID, token string) (PortalLink, error) {
	raw, hash, err := portalToken()
	if err != nil {
		return PortalLink{}, err
	}
	now := s.now()
	var result PortalLink
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var id, email string
		var expires time.Time
		var used *time.Time
		err := tx.QueryRow(ctx, `SELECT id::text,email,expires_at,used_at FROM portal_links
			WHERE workspace_id=$1 AND token_hash=$2 FOR UPDATE`, workspaceID, portalHash(token)).Scan(&id, &email, &expires, &used)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			result.Status = "unknown"
			return nil
		case err != nil:
			return err
		case used != nil:
			result.Status = "used"
			return nil
		case !expires.After(now):
			result.Status = "expired"
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE portal_links SET used_at=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, id, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO portal_sessions(workspace_id,email,token_hash,expires_at) VALUES($1,$2,$3,$4)`,
			workspaceID, email, hash, now.Add(PortalSessionLifetime)); err != nil {
			return err
		}
		result = PortalLink{Status: "ok", Session: raw, Email: email}
		return nil
	})
	if err != nil {
		return PortalLink{}, fmt.Errorf("redeem portal link: %w", err)
	}
	return result, nil
}

// PortalSession is the address a live session signs in, or "" when the token is missing, revoked or
// past its time. The lookup is inside the workspace the page belongs to, so another workspace's
// session is nothing here.
func (s *Store) PortalSession(ctx context.Context, workspaceID, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	var email string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT email FROM portal_sessions WHERE workspace_id=$1 AND token_hash=$2
			AND revoked_at IS NULL AND expires_at>$3`, workspaceID, portalHash(token), s.now()).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return "", fmt.Errorf("read portal session: %w", err)
	}
	return email, nil
}

// RevokePortalSession ends a sign-in. A token that is already gone is not an error.
func (s *Store) RevokePortalSession(ctx context.Context, workspaceID, token string) error {
	if token == "" {
		return nil
	}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE portal_sessions SET revoked_at=$3 WHERE workspace_id=$1 AND token_hash=$2 AND revoked_at IS NULL`,
			workspaceID, portalHash(token), s.now())
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke portal session: %w", err)
	}
	return nil
}

// ListPortalTickets returns the newest requests the address can see, at most 100, and whether older
// ones were left off.
func (s *Store) ListPortalTickets(ctx context.Context, workspaceID, email string) ([]PortalTicketSummary, bool, error) {
	var items []PortalTicketSummary
	var more bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.number,c.number_series,c.display_ref,c.subject,c.status,c.updated_at
			FROM conversations c WHERE `+portalVisible+` ORDER BY c.updated_at DESC, c.number DESC LIMIT 101`, workspaceID, email)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (PortalTicketSummary, error) {
			var item PortalTicketSummary
			err := row.Scan(&item.Number, &item.Series, &item.Ref, &item.Subject, &item.Status, &item.Updated)
			item.Updated = item.Updated.UTC()
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list portal tickets: %w", err)
	}
	if len(items) > 100 {
		more, items = true, items[:100]
	}
	return items, more, nil
}

// PortalTicket returns one request the address can see, with its public messages (never a note).
func (s *Store) PortalTicket(ctx context.Context, workspaceID, email, series string, number int64) (PortalTicket, error) {
	var ticket PortalTicket
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `SELECT c.id::text,c.number,c.number_series,c.display_ref,c.subject,c.status,c.updated_at
			FROM conversations c WHERE `+portalVisible+` AND c.number=$3 AND c.number_series=$4
			ORDER BY c.updated_at DESC LIMIT 1`, workspaceID, email, number, series).
			Scan(&id, &ticket.Number, &ticket.Series, &ticket.Ref, &ticket.Subject, &ticket.Status, &ticket.Updated)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPortalTicketNotFound
		}
		if err != nil {
			return err
		}
		ticket.Updated = ticket.Updated.UTC()
		return readPortalMessages(ctx, tx, workspaceID, id, &ticket)
	})
	if err != nil {
		return PortalTicket{}, err
	}
	return ticket, nil
}

// CreatePortalTicket opens a request from a signed-in address. The address is the contact's, found or
// created, and the message records via portal so a trial can answer it. The number is the one the
// portal addresses the request by.
func (s *Store) CreatePortalTicket(ctx context.Context, workspaceID, email, name, subject, body string) (PortalTicketSummary, error) {
	var created PortalTicketSummary
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		contactID, err := contactForEmail(ctx, tx, workspaceID, name, email)
		if err != nil {
			return err
		}
		now := s.now()
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel)
			VALUES($1,$2,$3,$4,$4,$5) RETURNING id::text,number,number_series,display_ref,status,updated_at`,
			workspaceID, contactID, subject, now, domain.ChannelPortal).
			Scan(&id, &created.Number, &created.Series, &created.Ref, &created.Status, &created.Updated); err != nil {
			return err
		}
		created.Subject = subject
		created.Updated = created.Updated.UTC()
		return insertPortalMessage(ctx, tx, workspaceID, id, email, body, now)
	})
	if err != nil {
		return PortalTicketSummary{}, fmt.Errorf("create portal ticket: %w", err)
	}
	return created, nil
}

// ReplyToPortalTicket adds the requester's reply to a request they can see, and reopens it when it
// was resolved or closed. A request they cannot see is ErrPortalTicketNotFound and nothing is stored.
func (s *Store) ReplyToPortalTicket(ctx context.Context, workspaceID, email, series string, number int64, body string) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var id, status string
		err := tx.QueryRow(ctx, `SELECT c.id::text,c.status FROM conversations c WHERE `+portalVisible+`
			AND c.number=$3 AND c.number_series=$4 ORDER BY c.updated_at DESC LIMIT 1 FOR UPDATE OF c`,
			workspaceID, email, number, series).Scan(&id, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPortalTicketNotFound
		}
		if err != nil {
			return err
		}
		if err := setStatusCause(ctx, tx, statusChange{Cause: CauseCustomer}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN status IN ('resolved','closed') THEN 'open' ELSE status END,
			updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
			return err
		}
		return insertPortalMessage(ctx, tx, workspaceID, id, email, body, s.now())
	})
	if errors.Is(err, ErrPortalTicketNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("reply to portal ticket: %w", err)
	}
	return nil
}

func insertPortalMessage(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, email, body string, at time.Time) error {
	meta, err := json.Marshal(domain.IncomingEmail{From: email, Authentication: "pass", Via: domain.ViaPortal, Attachments: []domain.EmailAttachment{}})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,created_at) VALUES($1,$2,'incoming',$3,$4,$5)`,
		workspaceID, conversationID, body, meta, at)
	return err
}

func readPortalMessages(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, ticket *PortalTicket) error {
	rows, err := tx.Query(ctx, `SELECT m.kind,m.body,m.created_at,COALESCE(NULLIF(btrim(a.display_name),''),'')
		FROM messages m LEFT JOIN agents a ON a.workspace_id=m.workspace_id AND a.id=m.author_agent_id
		WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.kind IN ('incoming','outgoing')
		ORDER BY m.created_at DESC, m.id DESC LIMIT 201`, workspaceID, conversationID)
	if err != nil {
		return err
	}
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PortalMessage, error) {
		var message PortalMessage
		err := row.Scan(&message.Kind, &message.Body, &message.At, &message.Author)
		message.At = message.At.UTC()
		return message, err
	})
	if err != nil {
		return err
	}
	if len(messages) > 200 {
		ticket.More = true
		messages = messages[:200]
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	ticket.Messages = messages
	return nil
}
