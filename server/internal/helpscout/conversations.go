package helpscout

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func (r *run) importConversations(ctx context.Context) error {
	for index, ref := range r.refs {
		if err := r.importConversation(ctx, ref); err != nil {
			return err
		}
		r.progress.tick("importing conversations", index+1, len(r.refs))
	}
	return nil
}

// unchanged reports whether a finished conversation needs no new read: Help Scout's update time has
// not moved and the thread count is the same (a customer reply does not always move userUpdatedAt).
func unchanged(entry ledgerEntry, found bool, ref hsConversation) bool {
	return found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil &&
		!ref.updatedAt().After(*entry.SourceUpdatedAt) && entry.Meta.Threads == ref.Threads
}

// importConversation brings one conversation over with its threads, attachments and images. Every
// child is idempotent by Help Scout id, so an interrupted conversation resumes where it stopped.
// Only cancellation and ledger errors are returned.
func (r *run) importConversation(ctx context.Context, ref hsConversation) error {
	key := strconv.FormatInt(ref.ID, 10)
	entry, found, err := r.ledger.get(ctx, entityTicket, key)
	if err != nil || unchanged(entry, found, ref) {
		return err
	}
	threads, err := r.fetchThreads(ctx, ref.ID)
	if err != nil {
		return r.conversationFailed(ctx, key, fmt.Errorf("read threads: %w", err))
	}
	contactID, err := r.contactFor(ctx, ref)
	if err != nil {
		return r.conversationFailed(ctx, key, fmt.Errorf("import customer %d: %w", ref.PrimaryCustomer.ID, err))
	}
	var existing *string
	if found {
		existing = entry.TargetID
	}
	meta := itemMeta{Threads: ref.Threads, ThreadsRead: len(threads), Contact: strconv.FormatInt(ref.PrimaryCustomer.ID, 10), Tags: tagNames(ref.Tags)}
	conversationID, err := r.writeConversation(ctx, ref, contactID, existing, meta)
	if err != nil {
		return r.conversationFailed(ctx, key, err)
	}
	failed := 0
	for _, thread := range threads {
		threadKey := strconv.FormatInt(thread.ID, 10)
		switch {
		case thread.Type == "lineitem":
			meta.LineItems++
			continue
		case thread.State == "draft":
			meta.Drafts = append(meta.Drafts, threadKey)
			continue
		}
		meta.Messages++
		files := []hsAttachment{}
		for _, file := range thread.Embedded.Attachments {
			if file.State == "virus" {
				meta.Virus = append(meta.Virus, strconv.FormatInt(file.ID, 10))
				continue
			}
			files = append(files, file)
		}
		meta.Attachments += len(files)
		meta.InlineImages += len(r.inlineImages(thread.Body))
		failed += r.importThread(ctx, ref, conversationID, thread, files)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.internalErr != nil {
		return r.internalErr
	}
	status, errText := ledgerDone, ""
	if failed > 0 {
		status, errText = ledgerFailed, fmt.Sprintf("%d item(s) of this conversation failed; see their entries", failed)
	}
	updated := ref.updatedAt()
	return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: key, targetID: &conversationID,
		status: status, errText: errText, sourceUpdatedAt: &updated, meta: &meta})
}

func (r *run) conversationFailed(ctx context.Context, key string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.itemFailed(ctx, entityTicket, key, "", cause)
	return r.internalErr
}

func (r *run) fetchThreads(ctx context.Context, conversationID int64) ([]hsThread, error) {
	var threads []hsThread
	_, err := listAll(ctx, r.client, "/v2/conversations/"+strconv.FormatInt(conversationID, 10)+"/threads", nil, "threads", func(t hsThread) error {
		threads = append(threads, t)
		return nil
	})
	return threads, err
}

func tagNames(tags []hsTag) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		if name := strings.TrimSpace(tag.Tag); name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

var conversationSources = map[string]string{"email": "email", "phone": "phone", "chat": "chat"}

func conversationOrigin(ref hsConversation) *domain.ConversationOrigin {
	source := "other"
	if name, ok := conversationSources[ref.Type]; ok {
		source = name
	}
	return &domain.ConversationOrigin{System: "helpscout", ExternalID: strconv.FormatInt(ref.Number, 10), Source: &source,
		CC: list(ref.CC), ForwardedTo: []string{}}
}

func list(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func subjectOrDefault(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return "(no subject)"
	}
	return subject
}
