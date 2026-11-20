package freshdesk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// entityKBFile is a file attached to an article, stored as a file of the media library; its source
// id is <article id>:<attachment id> (or <article id>:<language>:<attachment id>).
const entityKBFile = "kb_file"

// kbFileSlot is one file of an article's list, in order: the library item the ledger holds, or the
// stored bytes of one that saveKBFiles writes.
type kbFileSlot struct {
	ledgerID, id, contentType, name, text string
	object                                *attachments.Object
}

// fetchKBFiles brings the files of one article or translation (prefix is its ledger key) over in their
// order. A file Colliedesk refuses (its type, its size, the article's total) or one that cannot be
// downloaded is recorded as failed with the reason and counted; the article lists the others.
func (r *run) fetchKBFiles(ctx context.Context, prefix string, files []fdAttachment) ([]kbFileSlot, int, error) {
	var slots []kbFileSlot
	failed, total := 0, int64(0)
	for _, file := range files {
		ledgerID := prefix + ":" + strconv.FormatInt(file.ID, 10)
		entry, found, err := r.ledger.get(ctx, entityKBFile, ledgerID)
		if err != nil {
			return nil, 0, err
		}
		if found && entry.Status == ledgerDone && entry.TargetID != nil {
			slots = append(slots, kbFileSlot{ledgerID: ledgerID, id: *entry.TargetID})
			total += file.reportedSize()
			continue
		}
		slot, err := r.fetchKBFile(ctx, ledgerID, file, len(slots), total)
		switch {
		case ctx.Err() != nil:
			return nil, 0, ctx.Err()
		case err != nil:
			r.itemFailed(ctx, entityKBFile, ledgerID, "", fmt.Errorf("file %q: %w", file.Name, err))
			failed++
			continue
		}
		slots = append(slots, slot)
		total += slot.object.Size
	}
	return slots, failed, r.internalErr
}

// fetchKBFile checks one file against the media library's rules, downloads it and stores its bytes.
func (r *run) fetchKBFile(ctx context.Context, ledgerID string, file fdAttachment, listed int, total int64) (kbFileSlot, error) {
	name, err := domain.CleanKBMediaText("name", file.Name, domain.MaxKBMediaName)
	switch {
	case err != nil:
		return kbFileSlot{}, err
	case listed >= domain.MaxKBAttachments:
		return kbFileSlot{}, fmt.Errorf("an article lists at most %d files", domain.MaxKBAttachments)
	case file.reportedSize() > domain.MaxKBFileBytes:
		return kbFileSlot{}, fmt.Errorf("it is %d bytes, over the 25 MiB limit for one file", file.reportedSize())
	case total+file.reportedSize() > domain.MaxKBArticleFileBytes:
		return kbFileSlot{}, errors.New("the article's files would come to more than 100 MiB")
	}
	if err := media.CheckFileName(name); err != nil {
		return kbFileSlot{}, err
	}
	if err := r.checkDiskSpace(file.reportedSize()); err != nil {
		return kbFileSlot{}, err
	}
	body, _, _, err := r.client.download(ctx, file.AttachmentURL)
	if err != nil {
		return kbFileSlot{}, fmt.Errorf("download %s: %w", redact(file.AttachmentURL), err)
	}
	data, err := io.ReadAll(io.LimitReader(body, domain.MaxKBFileBytes+1))
	_ = body.Close()
	switch {
	case err != nil:
		return kbFileSlot{}, fmt.Errorf("download %s: %w", redact(file.AttachmentURL), err)
	case len(data) > domain.MaxKBFileBytes:
		return kbFileSlot{}, errors.New("it is over the 25 MiB limit for one file")
	case total+int64(len(data)) > domain.MaxKBArticleFileBytes:
		return kbFileSlot{}, errors.New("the article's files would come to more than 100 MiB")
	}
	contentType, err := media.InspectFile(name, data)
	if err != nil {
		return kbFileSlot{}, err
	}
	text := media.FileText(contentType, bytes.NewReader(data), int64(len(data)))
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return kbFileSlot{}, fmt.Errorf("store: %w", err)
	}
	return kbFileSlot{ledgerID: ledgerID, contentType: contentType, name: name, text: text, object: &object}, nil
}

// kbPartsFailed says how many images and files of an article or translation (what) failed, "" for none.
func kbPartsFailed(what string, images, files int) string {
	var parts []string
	if images > 0 {
		parts = append(parts, fmt.Sprintf("%d image(s)", images))
	}
	if files > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s)", files))
	}
	if len(parts) == 0 {
		return ""
	}
	text := strings.Join(parts, " and ") + " of this " + what + " failed; see their entries"
	if images > 0 {
		text += ". It keeps the Freshdesk links of its images"
	}
	return text + " until a rerun brings them over"
}

// saveKBFiles writes the library items of the files stored for one article in tx and lists every file
// of slots, in order, as the live list of the article (locale nil) or of its translation.
func (r *run) saveKBFiles(ctx context.Context, tx pgx.Tx, articleID string, locale *string, slots []kbFileSlot) error {
	ids := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.object != nil {
			id, created, err := store.ImportKBFile(ctx, tx, r.opts.Storage, r.workspaceID, *slot.object, slot.contentType, slot.name, slot.text)
			if err != nil {
				return err
			}
			if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBFile, sourceID: slot.ledgerID, targetID: &id, status: ledgerDone, meta: &ticketMeta{Created: created}}); err != nil {
				return err
			}
			slot.id = id
		}
		ids = append(ids, slot.id)
	}
	return store.ImportKBAttachments(ctx, tx, r.workspaceID, articleID, locale, ids)
}
