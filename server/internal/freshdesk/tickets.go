package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (r *run) importTickets(ctx context.Context) error {
	for index, ref := range r.refs {
		if err := r.importTicket(ctx, ref); err != nil {
			return err
		}
		r.progress.tick("importing tickets", index+1, len(r.refs))
	}
	return r.flushStats(ctx)
}

// importTicket brings one ticket over with its messages, attachments and time
// entries. Every child is idempotent by Freshdesk id, so an interrupted ticket
// resumes where it stopped. Only cancellation and ledger errors are returned.
func (r *run) importTicket(ctx context.Context, ref ticketRef) error {
	key := strconv.FormatInt(ref.id, 10)
	entry, found, err := r.ledger.get(ctx, entityTicket, key)
	if err != nil {
		return err
	}
	if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !ref.updatedAt.After(*entry.SourceUpdatedAt) {
		if ref.stats != nil && entry.TargetID != nil {
			return r.fillStats(ctx, *entry.TargetID, ref.stats, ref.status)
		}
		return nil
	}
	ticket, err := r.fetchTicket(ctx, ref)
	if errors.Is(err, errNotFound) {
		r.deleted = append(r.deleted, key)
		return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: key, status: ledgerSkipped, errText: skipDeleted})
	}
	if err != nil {
		return r.ticketFailed(ctx, key, fmt.Errorf("read ticket: %w", err))
	}
	conversations, err := r.fetchConversations(ctx, ref)
	if err != nil {
		return r.ticketFailed(ctx, key, fmt.Errorf("read conversations: %w", err))
	}
	var entries []fdTimeEntry
	if ref.archived {
		r.warn("archived_time_entries_unavailable", "Freshdesk documents no time entry endpoint for archived tickets; check them in Freshdesk before closing the account.", key)
	} else if err := listAll(ctx, r.client, r.endpoint("/api/v2/tickets/"+key+"/time_entries", pageQuery()), func(e fdTimeEntry) error {
		entries = append(entries, e)
		return nil
	}); err != nil {
		return r.ticketFailed(ctx, key, fmt.Errorf("read time entries: %w", err))
	}
	contactID, err := r.requesterContact(ctx, ticket.RequesterID)
	if err != nil {
		return r.ticketFailed(ctx, key, fmt.Errorf("import requester %d: %w", ticket.RequesterID, err))
	}
	var existing *string
	if found {
		existing = entry.TargetID
	}
	ticket.Stats = ref.stats
	conversationID, err := r.writeConversation(ctx, ticket, contactID, existing, ref.archived)
	if err != nil {
		return r.ticketFailed(ctx, key, err)
	}

	meta := ticketMeta{Archived: ref.archived, Messages: 1 + len(conversations), TimeEntries: len(entries)}
	refresh := r.refresher(ref)
	failed := r.importMessage(ctx, key, conversationID, entityDescription, key, r.descriptionMessage(ticket), ticket.Attachments, refresh)
	countFiles(&meta, ticket.Attachments)
	meta.InlineImages += len(r.inlineImages(ticket.Description))
	for _, conversation := range conversations {
		id := strconv.FormatInt(conversation.ID, 10)
		failed += r.importMessage(ctx, key, conversationID, entityMessage, id, r.conversationMessage(conversation), conversation.Attachments, refresh)
		countFiles(&meta, conversation.Attachments)
		meta.InlineImages += len(r.inlineImages(conversation.Body))
	}
	for _, timeEntry := range entries {
		failed += r.importTimeEntry(ctx, key, conversationID, timeEntry)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.internalErr != nil {
		return r.internalErr
	}
	status, errText := ledgerDone, ""
	if failed > 0 {
		status, errText = ledgerFailed, fmt.Sprintf("%d item(s) of this ticket failed; see their entries", failed)
	}
	updated := ticket.UpdatedAt.UTC()
	return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: key, targetID: &conversationID,
		status: status, errText: errText, sourceUpdatedAt: &updated, meta: &meta})
}

func (r *run) ticketFailed(ctx context.Context, key string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.client.fatalErr(); err != nil {
		return err
	}
	r.itemFailed(ctx, entityTicket, key, "", cause)
	return r.internalErr
}

func (r *run) fetchTicket(ctx context.Context, ref ticketRef) (fdTicket, error) {
	path := "/api/v2/tickets/" + strconv.FormatInt(ref.id, 10)
	if ref.archived {
		path = "/api/v2/tickets/archived/" + strconv.FormatInt(ref.id, 10)
	}
	var ticket fdTicket
	_, err := r.client.getJSON(ctx, r.endpoint(path, nil), &ticket)
	return ticket, err
}

func (r *run) fetchConversations(ctx context.Context, ref ticketRef) ([]fdConversation, error) {
	path := "/api/v2/tickets/" + strconv.FormatInt(ref.id, 10) + "/conversations"
	if ref.archived {
		path = "/api/v2/tickets/archived/" + strconv.FormatInt(ref.id, 10) + "/conversations"
	}
	var items []fdConversation
	err := listAll(ctx, r.client, r.endpoint(path, pageQuery()), func(c fdConversation) error {
		items = append(items, c)
		return nil
	})
	return items, err
}

// refresher re-reads a ticket to get a fresh pre-signed URL for an attachment
// whose URL expired before it could be downloaded.
func (r *run) refresher(ref ticketRef) func(context.Context, int64) (fdAttachment, error) {
	return func(ctx context.Context, attachmentID int64) (fdAttachment, error) {
		ticket, err := r.fetchTicket(ctx, ref)
		if err != nil {
			return fdAttachment{}, err
		}
		for _, file := range ticket.Attachments {
			if file.ID == attachmentID {
				return file, nil
			}
		}
		conversations, err := r.fetchConversations(ctx, ref)
		if err != nil {
			return fdAttachment{}, err
		}
		for _, conversation := range conversations {
			for _, file := range conversation.Attachments {
				if file.ID == attachmentID {
					return file, nil
				}
			}
		}
		return fdAttachment{}, fmt.Errorf("attachment %d no longer listed on the ticket: %w", attachmentID, errNotFound)
	}
}

func countFiles(meta *ticketMeta, files []fdAttachment) {
	for _, file := range files {
		meta.Attachments++
		meta.AttachmentBytes += file.reportedSize()
	}
}

func subjectOrDefault(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return "(no subject)"
	}
	return subject
}
