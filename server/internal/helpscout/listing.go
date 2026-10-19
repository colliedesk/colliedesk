package helpscout

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// selectMailbox picks the inbox to import. The ledger is keyed by its id, which Help Scout never
// reuses, so renaming the inbox never imports it a second time.
func (r *run) selectMailbox(ctx context.Context) error {
	var mailboxes []hsMailbox
	if _, err := listAll(ctx, r.client, "/v2/mailboxes", nil, "mailboxes", func(m hsMailbox) error {
		mailboxes = append(mailboxes, m)
		return nil
	}); err != nil {
		return fmt.Errorf("list Help Scout inboxes: %w", err)
	}
	choices := make([]string, 0, len(mailboxes))
	for _, m := range mailboxes {
		if m.ID == r.opts.MailboxID || (r.opts.MailboxID == 0 && len(mailboxes) == 1) {
			r.mailbox = m
			r.ledger.source = "helpscout:mailbox:" + strconv.FormatInt(m.ID, 10)
			return nil
		}
		choices = append(choices, fmt.Sprintf("%d (%s)", m.ID, m.Name))
	}
	switch {
	case len(mailboxes) == 0:
		return fmt.Errorf("the Help Scout account has no inbox this app can read")
	case r.opts.MailboxID != 0:
		return fmt.Errorf("Help Scout inbox %d not found; inboxes: %s", r.opts.MailboxID, strings.Join(choices, ", "))
	}
	return fmt.Errorf("the Help Scout account has %d inboxes; choose one with --mailbox: %s", len(mailboxes), strings.Join(choices, ", "))
}

// loadUsers reads the Help Scout users and matches them by email to Colliedesk agents.
func (r *run) loadUsers(ctx context.Context) error {
	local, inactive := map[string]string{}, map[string]bool{}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id::text,lower(email),deactivated_at IS NOT NULL FROM agents WHERE workspace_id=$1`, r.workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	for rows.Next() {
		var id, email string
		var deactivated bool
		if err := rows.Scan(&id, &email, &deactivated); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return fmt.Errorf("scan Colliedesk agent: %w", err)
		}
		local[email], inactive[id] = id, deactivated
	}
	rows.Close()
	_ = tx.Rollback(ctx) // read only; end it before the Help Scout calls below
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	_, err = listAll(ctx, r.client, "/v2/users", nil, "users", func(user hsUser) error {
		r.users[user.ID] = user
		if id, ok := local[strings.ToLower(strings.TrimSpace(user.Email))]; ok {
			r.agentTargets[user.ID], r.deactivated[user.ID] = id, inactive[id]
		} else {
			r.warn("agent_not_matched", "Help Scout user has no Colliedesk agent with the same email; their replies keep the author name only. Invite them, then re-run to keep future items linked.", strconv.FormatInt(user.ID, 10))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("list Help Scout users: %w", err)
	}
	return nil
}

// listConversations lists the inbox oldest first, so conversations created while the list runs
// land on the last page instead of shifting earlier pages. Spam is listed separately: Help Scout
// keeps it out of the other statuses.
func (r *run) listConversations(ctx context.Context) error {
	query := url.Values{"mailbox": {strconv.FormatInt(r.mailbox.ID, 10)}, "status": {"all"}, "sortField": {"createdAt"}, "sortOrder": {"asc"}}
	if r.opts.Since != nil {
		query.Set("modifiedSince", r.opts.Since.UTC().Format(time.RFC3339))
	}
	seen := map[int64]bool{}
	total, err := listAll(ctx, r.client, "/v2/conversations", query, "conversations", func(c hsConversation) error {
		r.listed++
		if !seen[c.ID] {
			seen[c.ID] = true
			r.classify(c)
		}
		r.progress.tick("listing conversations", r.listed, 0)
		return nil
	})
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}
	r.verification.Conversations = Verification{Verified: r.listed == total && len(seen) == r.listed,
		Method: "Every page of the conversation list was read and counted, and each conversation's thread count was compared with the threads read.",
		Detail: fmt.Sprintf("Listed %d (%d distinct); Help Scout reports %d (page.totalElements of the last page).", r.listed, len(seen), total)}
	query.Set("status", "spam")
	_, err = listAll(ctx, r.client, "/v2/conversations", query, "conversations", func(c hsConversation) error {
		if !seen[c.ID] {
			seen[c.ID] = true
			r.spam = append(r.spam, strconv.FormatInt(c.ID, 10))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("list spam conversations: %w", err)
	}
	return nil
}

func (r *run) classify(c hsConversation) {
	id := strconv.FormatInt(c.ID, 10)
	switch {
	case c.Status == "spam":
		r.spam = append(r.spam, id)
	case c.State == "deleted":
		r.deleted = append(r.deleted, id)
	case c.State == "draft":
		r.drafts = append(r.drafts, id)
	default:
		r.refs = append(r.refs, c)
	}
}
