package freshdesk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// maxInlineImageBytes bounds one image embedded into the HTML original as a data: URI, the same
// budget the email channel uses for the images of a message.
const maxInlineImageBytes = 5 << 20

var imgSrc = regexp.MustCompile(`(?is)(<img\b[^>]*?\bsrc\s*=\s*)(?:"([^"]*)"|'([^']*)')`)

// mapImageSources rewrites the src of every <img>. fn gets the attribute value with HTML entities
// decoded and returns the new value, or false to leave the tag as written.
func mapImageSources(markup string, fn func(src string) (string, bool)) string {
	return imgSrc.ReplaceAllStringFunc(markup, func(tag string) string {
		parts := imgSrc.FindStringSubmatch(tag)
		src := parts[2]
		if src == "" {
			src = parts[3]
		}
		replacement, ok := fn(html.UnescapeString(src))
		if !ok {
			return tag
		}
		return parts[1] + `"` + replacement + `"`
	})
}

type imageRef struct{ url, key string }

// hostedImageURL returns the URL of an image that Freshdesk hosts: the account host (also a custom
// support domain), Freshdesk's own domains and its public asset bucket. Any other host is not ours
// to fetch from, so such an image stays a link.
func (r *run) hostedImageURL(src string) (*url.URL, bool) {
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.EqualFold(u.Host, r.client.base.Host), r.opts.hooks.allowDial[u.Host]:
		return u, true
	case host == "freshdesk.com", strings.HasSuffix(host, ".freshdesk.com"):
		return u, true
	case isS3Host(host) && strings.HasPrefix(u.Path, "/cdn.freshdesk.com/"):
		return u, true
	case strings.HasPrefix(host, "cdn.freshdesk.com.s3") && strings.HasSuffix(host, ".amazonaws.com"):
		return u, true
	}
	return nil, false
}

func isS3Host(host string) bool {
	return host == "s3.amazonaws.com" || (strings.HasPrefix(host, "s3") && strings.HasSuffix(host, ".amazonaws.com"))
}

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
	mapImageSources(markup, func(src string) (string, bool) {
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

// importInlineImages brings the Freshdesk-hosted images of one message over and returns how many
// failed (already recorded in the ledger). A failed image keeps its original link in the HTML.
func (r *run) importInlineImages(ctx context.Context, ticketKey, messageID, entity, sourceID, markup string) int {
	failed := 0
	for _, ref := range r.inlineImages(markup) {
		id := inlineImageID(entity, sourceID, ref.key)
		err := r.importInlineImage(ctx, ticketKey, messageID, id, ref)
		switch {
		case err == nil:
		case errors.Is(err, errDiskFull):
			if r.internalErr == nil {
				r.internalErr = err
			}
			return failed + 1
		default:
			r.itemFailed(ctx, entityInlineImage, id, ticketKey, err)
			failed++
		}
	}
	return failed
}

// fetchImage downloads one image, refusing anything that is not an image or is over the budget.
func (r *run) fetchImage(ctx context.Context, target string) ([]byte, string, error) {
	body, length, header, err := r.client.download(ctx, target)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(header, ";", 2)[0]))
	switch {
	case !strings.HasPrefix(contentType, "image/"):
		return nil, "", fmt.Errorf("the response is %q, not an image", contentType)
	case length > maxInlineImageBytes:
		return nil, "", fmt.Errorf("the image is %d bytes, over the %d byte limit for images in message HTML", length, maxInlineImageBytes)
	}
	data, err := io.ReadAll(io.LimitReader(body, maxInlineImageBytes+1))
	switch {
	case err != nil:
		return nil, "", fmt.Errorf("read image: %w", err)
	case len(data) > maxInlineImageBytes:
		return nil, "", fmt.Errorf("the image is over the %d byte limit for images in message HTML", maxInlineImageBytes)
	}
	return data, contentType, nil
}

func (r *run) importInlineImage(ctx context.Context, ticketKey, messageID, id string, ref imageRef) error {
	entry, found, err := r.ledger.get(ctx, entityInlineImage, id)
	if err != nil || (found && entry.Status == ledgerDone) {
		return err
	}
	if err := r.checkDiskSpace(0); err != nil {
		return err
	}
	data, contentType, err := r.fetchImage(ctx, ref.url)
	if err != nil {
		return fmt.Errorf("download image %s: %w; the original link is kept in the message HTML", redact(ref.url), err)
	}
	if err := r.checkDiskSpace(int64(len(data))); err != nil {
		return err
	}
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("store image: %w", err)
	}
	input := store.NewAttachment{MessageID: messageID, Filename: "inline-image-" + ref.key + imageExtension(contentType), ContentType: contentType, Object: object}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin inline image import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := store.VerifyObjects(ctx, tx, r.opts.Storage, object.Key); err != nil {
		return err
	}
	var targetID string
	replaced := false
	if found && entry.TargetID != nil {
		if replaced, err = store.ReplaceAttachmentBytes(ctx, tx, r.workspaceID, *entry.TargetID, input); err != nil {
			return err
		}
		targetID = *entry.TargetID
	}
	if !replaced {
		item, err := store.InsertAttachment(ctx, tx, r.workspaceID, input)
		if err != nil {
			return err
		}
		targetID = item.ID
	}
	if err := r.embedImage(ctx, tx, messageID, ref, "data:"+contentType+";base64,"+base64.StdEncoding.EncodeToString(data)); err != nil {
		return err
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityInlineImage, sourceID: id, parentID: ticketKey, targetID: &targetID, status: ledgerDone}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inline image import: %w", err)
	}
	return nil
}

// embedImage points the stored HTML original at the stored image as a data: URI, so the locked frame
// (img-src data:, nothing loaded from the network) shows it after Freshdesk closes. It runs in the
// transaction that records the image, so the HTML and the ledger never disagree.
func (r *run) embedImage(ctx context.Context, tx store.Querier, messageID string, ref imageRef, dataURI string) error {
	var current *string
	if err := tx.QueryRow(ctx, `SELECT body_html FROM messages WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, r.workspaceID, messageID).Scan(&current); err != nil {
		return fmt.Errorf("read message HTML: %w", err)
	}
	if current == nil {
		return nil
	}
	updated := mapImageSources(*current, func(src string) (string, bool) {
		u, ok := r.hostedImageURL(src)
		return dataURI, ok && imageKey(u) == ref.key
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

func imageExtension(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	}
	return ""
}

// measureImage returns the size of an image the sizing pass would store, 0 when it cannot be read:
// the real import then fails that image and lists it.
func (r *run) measureImage(ctx context.Context, ref imageRef) (int64, error) {
	data, _, err := r.fetchImage(ctx, ref.url)
	if err != nil {
		return 0, ctx.Err()
	}
	return int64(len(data)), nil
}
