package freshdesk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	// maxInlineImageBytes bounds one image; maxMessageImageBytes bounds the images embedded as
	// data: URIs in one message's HTML, the budget the email channel uses (email/files.go).
	maxInlineImageBytes  = 5 << 20
	maxMessageImageBytes = 5 << 20
)

type imageRef struct{ url, key string }

// imageKey identifies an image across reads. Pre-signed URLs change their signature and expiry on
// every read of the ticket, so those parameters are left out.
func imageKey(u *url.URL) string {
	kept := url.Values{}
	for name, values := range u.Query() {
		lower := strings.ToLower(name)
		switch lower {
		case "signature", "expires", "awsaccesskeyid", "policy", "key-pair-id", "se", "sig":
			continue
		}
		if !strings.HasPrefix(lower, "x-amz-") {
			kept[name] = values
		}
	}
	sum := sha256.Sum256([]byte(strings.ToLower(u.Host) + u.EscapedPath() + "?" + kept.Encode()))
	return hex.EncodeToString(sum[:8])
}

// inlineImages lists the distinct Freshdesk-hosted images of a message HTML, in order.
func (r *run) inlineImages(markup string) []imageRef {
	var refs []imageRef
	seen := map[string]bool{}
	importkit.MapImageSources(markup, func(src string) (string, bool) {
		if u, ok := r.hostedImageURL(src); ok {
			if key := imageKey(u); !seen[key] {
				seen[key] = true
				refs = append(refs, imageRef{url: u.String(), key: key})
			}
		}
		return "", false
	})
	return refs
}

// inlineImageID is the ledger id of one image of one message. The message entity is part of it
// because a ticket id and a conversation id can be equal.
func inlineImageID(entity, sourceID, key string) string { return entity + ":" + sourceID + ":" + key }

// openImage starts a download that refuses to leave the allowed hosts, even through redirects.
func (r *run) openImage(ctx context.Context, target string) (io.ReadCloser, int64, string, error) {
	return r.client.download(withRedirectGuard(ctx, r.allowedRedirectURL), target)
}

// fetchImage downloads one image and returns its bytes and fixed media type.
func (r *run) fetchImage(ctx context.Context, target string) ([]byte, string, error) {
	body, length, header, err := r.openImage(ctx, target)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	if length > maxInlineImageBytes {
		return nil, "", fmt.Errorf("the image is %d bytes, over the %d byte limit for one image", length, maxInlineImageBytes)
	}
	data, err := io.ReadAll(io.LimitReader(body, maxInlineImageBytes+1))
	switch {
	case err != nil:
		return nil, "", fmt.Errorf("read image: %w", err)
	case len(data) > maxInlineImageBytes:
		return nil, "", fmt.Errorf("the image is over the %d byte limit for one image", maxInlineImageBytes)
	}
	mediaType, err := importkit.ImageType(header, data)
	return data, mediaType, err
}

// storedImage is an image already written to storage, waiting for its rows.
type storedImage struct {
	ref         imageRef
	id          string
	entry       ledgerEntry
	found       bool
	contentType string
	object      attachments.Object
	// dataURI is set when the image fits the message budget and goes into the HTML.
	dataURI string
}

// importInlineImages brings the Freshdesk-hosted images of one message over and returns how many
// failed (already recorded in the ledger). A failed image keeps its original link in the HTML.
// All images of the message are committed together with one rewrite of its HTML.
func (r *run) importInlineImages(ctx context.Context, ticketKey, messageID, entity, sourceID, markup string) int {
	if r.looksHosted(markup) {
		r.warn("inline_images_not_rehosted", "Message HTML has images that name Freshdesk but are on a host the importer does not fetch from (a custom support domain, another Freshdesk account). They stay links and break when the Freshdesk account closes; save them in Freshdesk before closing it.", ticketKey)
	}
	refs := r.inlineImages(markup)
	if len(refs) == 0 {
		return 0
	}
	budget, err := r.imageBudget(ctx, messageID)
	if err != nil {
		r.itemFailed(ctx, entityInlineImage, inlineImageID(entity, sourceID, refs[0].key), ticketKey, err)
		return len(refs)
	}
	failed := 0
	var pending []storedImage
	for _, ref := range refs {
		id := inlineImageID(entity, sourceID, ref.key)
		image, err := r.downloadInlineImage(ctx, id, ref, &budget)
		switch {
		case err == nil && image == nil:
		case err == nil:
			pending = append(pending, *image)
		case errors.Is(err, errDiskFull):
			if r.internalErr == nil {
				r.internalErr = err
			}
			return failed + 1
		case errors.Is(err, errNotFound), errors.Is(err, errGone):
			// Freshdesk no longer has the file and never will: record it for the report, keep the link.
			if mark := r.ledger.mark(ctx, nil, ledgerMark{entity: entityInlineImage, sourceID: id, parentID: ticketKey, status: ledgerSkipped, errText: err.Error()}); mark != nil && r.internalErr == nil {
				r.internalErr = mark
			}
		default:
			r.itemFailed(ctx, entityInlineImage, id, ticketKey, err)
			failed++
		}
	}
	if len(pending) == 0 {
		return failed
	}
	if err := r.commitInlineImages(ctx, ticketKey, messageID, pending); err != nil {
		for _, image := range pending {
			r.itemFailed(ctx, entityInlineImage, image.id, ticketKey, err)
		}
		return failed + len(pending)
	}
	for _, image := range pending {
		if image.dataURI == "" {
			r.warn("inline_image_not_embedded", "An image is stored as a file on the message, but the message already holds 5 MiB of embedded images, so Show original keeps the Freshdesk link for it.", ticketKey)
		}
	}
	return failed
}

// imageBudget is what is left of the message's embedded-image budget after earlier runs.
func (r *run) imageBudget(ctx context.Context, messageID string) (int64, error) {
	var used int64
	err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(SUM(a.size),0) FROM attachments a JOIN import_items i
			ON i.workspace_id=a.workspace_id AND i.target_id=a.id AND i.entity='inline_image' AND i.status='done'
			WHERE a.workspace_id=$1 AND a.message_id=$2`, r.workspaceID, messageID).Scan(&used)
	})
	if err != nil {
		return 0, fmt.Errorf("read inline image budget: %w", err)
	}
	return max(maxMessageImageBytes-used, 0), nil
}

// downloadInlineImage downloads and stores one image. It returns nil when the ledger already holds it.
func (r *run) downloadInlineImage(ctx context.Context, id string, ref imageRef, budget *int64) (*storedImage, error) {
	entry, found, err := r.ledger.get(ctx, entityInlineImage, id)
	if err != nil || (found && entry.Status == ledgerDone) {
		return nil, err
	}
	if err := r.checkDiskSpace(0); err != nil {
		return nil, err
	}
	data, contentType, err := r.fetchImage(ctx, ref.url)
	if err != nil {
		return nil, fmt.Errorf("download image %s: %w; the original link is kept in the message HTML", redact(ref.url), err)
	}
	if err := r.checkDiskSpace(int64(len(data))); err != nil {
		return nil, err
	}
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("store image: %w", err)
	}
	image := &storedImage{ref: ref, id: id, entry: entry, found: found, contentType: contentType, object: object}
	if int64(len(data)) <= *budget {
		*budget -= int64(len(data))
		image.dataURI = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return image, nil
}

// commitInlineImages records the attachment rows and ledger rows of a message's images and points
// its stored HTML original at them as data: URIs, in one transaction and with one write of the HTML.
// The locked frame (img-src data:, nothing loaded from the network) then shows them after Freshdesk
// closes.
func (r *run) commitInlineImages(ctx context.Context, ticketKey, messageID string, images []storedImage) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin inline image import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	keys := make([]string, len(images))
	for i, image := range images {
		keys[i] = image.object.Key
	}
	if err := store.VerifyObjects(ctx, tx, r.opts.Storage, keys...); err != nil {
		return err
	}
	embeds := map[string]string{}
	for _, image := range images {
		input := store.NewAttachment{MessageID: messageID, Filename: "inline-image-" + image.ref.key + importkit.ImageExtension(image.contentType), ContentType: image.contentType, Object: image.object}
		targetID, err := r.saveAttachment(ctx, tx, image.entry, image.found, input)
		if err != nil {
			return err
		}
		if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityInlineImage, sourceID: image.id, parentID: ticketKey, targetID: &targetID, status: ledgerDone}); err != nil {
			return err
		}
		if image.dataURI != "" {
			embeds[image.ref.key] = image.dataURI
		}
	}
	if len(embeds) > 0 {
		if err := r.embedImages(ctx, tx, messageID, embeds); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inline image import: %w", err)
	}
	return nil
}

func (r *run) embedImages(ctx context.Context, tx store.Querier, messageID string, embeds map[string]string) error {
	var current *string
	if err := tx.QueryRow(ctx, `SELECT body_html FROM messages WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, r.workspaceID, messageID).Scan(&current); err != nil {
		return fmt.Errorf("read message HTML: %w", err)
	}
	if current == nil {
		return nil
	}
	updated := importkit.MapImageSources(*current, func(src string) (string, bool) {
		u, ok := r.hostedImageURL(src)
		if !ok {
			return "", false
		}
		uri, found := embeds[imageKey(u)]
		return uri, found
	})
	if updated == *current {
		return nil
	}
	var written string
	if err := tx.QueryRow(ctx, `UPDATE messages SET body_html=$3 WHERE workspace_id=$1 AND id=$2 RETURNING id::text`, r.workspaceID, messageID, updated).Scan(&written); err != nil {
		return fmt.Errorf("write message HTML: %w", err)
	}
	return nil
}

// measureImage returns the size of an image the sizing pass would store, or 0 when it cannot be
// read: the real import then fails that image and lists it. A announced length is trusted, so the
// body is not downloaded twice; the import checks the real bytes.
func (r *run) measureImage(ctx context.Context, ref imageRef) (int64, error) {
	body, length, _, err := r.openImage(ctx, ref.url)
	if err != nil {
		return 0, ctx.Err()
	}
	if length >= 0 {
		_ = body.Close()
		if length > maxInlineImageBytes {
			return 0, nil
		}
		return length, nil
	}
	_ = body.Close()
	data, _, err := r.fetchImage(ctx, ref.url)
	if err != nil {
		return 0, ctx.Err()
	}
	return int64(len(data)), nil
}
