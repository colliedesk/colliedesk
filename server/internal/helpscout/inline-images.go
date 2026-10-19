package helpscout

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// maxImageBytes bounds one image and the images embedded as data: URIs in one message, the budget
// the email channel and the Freshdesk importer use.
const maxImageBytes = 5 << 20

type imageRef struct{ url, key string }

// allowedImageURL says whether the importer may fetch an image from u: Help Scout's own hosts
// (helpscout.net and its subdomains). It checks the host only, so it also guards redirect hops.
func (r *run) allowedImageURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return r.opts.hooks.imageHosts[u.Host] || host == "helpscout.net" || strings.HasSuffix(host, ".helpscout.net")
}

func (r *run) hostedImageURL(src string) (*url.URL, bool) {
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || !r.allowedImageURL(u) {
		return nil, false
	}
	u.Scheme = "https"
	return u, true
}

func imageKey(u *url.URL) string {
	sum := sha256.Sum256([]byte(strings.ToLower(u.Host) + u.EscapedPath() + "?" + u.RawQuery))
	return hex.EncodeToString(sum[:8])
}

// inlineImages lists the distinct Help Scout-hosted images of a thread's HTML, in order.
func (r *run) inlineImages(markup string) []imageRef {
	var refs []imageRef
	seen := map[string]bool{}
	mailimage.MapSources(markup, func(src string) (string, bool) {
		if u, ok := r.hostedImageURL(src); ok {
			if key := imageKey(u); !seen[key] {
				seen[key] = true
				refs = append(refs, imageRef{url: u.String(), key: key})
			}
		} else if strings.Contains(strings.ToLower(src), "helpscout") && !strings.HasPrefix(src, "data:") {
			r.warn("inline_images_not_rehosted", "Thread HTML has an image that names Help Scout but is not on a Help Scout host the importer fetches from. It stays a link; save it before closing the Help Scout account.", "")
		}
		return "", false
	})
	return refs
}

func inlineImageID(threadKey, key string) string { return entityThread + ":" + threadKey + ":" + key }

type storedImage struct {
	ref             imageRef
	id, contentType string
	entry           ledgerEntry
	found           bool
	object          attachments.Object
	// dataURI is set when the image fits the message budget and goes into the HTML.
	dataURI string
}

// importInlineImages stores the hosted images of one thread and embeds them as data: URIs in its
// HTML, all in one transaction. It returns how many failed (recorded in the ledger). An image Help
// Scout no longer has (404, 410) is recorded as skipped and keeps its link.
func (r *run) importInlineImages(ctx context.Context, conversationKey, threadKey, messageID, markup string) int {
	refs := r.inlineImages(markup)
	if len(refs) == 0 {
		return 0
	}
	budget, err := r.imageBudget(ctx, messageID)
	if err != nil {
		r.itemFailed(ctx, entityInlineImage, inlineImageID(threadKey, refs[0].key), conversationKey, err)
		return len(refs)
	}
	failed := 0
	var pending []storedImage
	for _, ref := range refs {
		id := inlineImageID(threadKey, ref.key)
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
		case errors.Is(err, errGone):
			if mark := r.ledger.mark(ctx, nil, ledgerMark{entity: entityInlineImage, sourceID: id, parentID: conversationKey, status: ledgerSkipped, errText: err.Error()}); mark != nil && r.internalErr == nil {
				r.internalErr = mark
			}
		default:
			r.itemFailed(ctx, entityInlineImage, id, conversationKey, err)
			failed++
		}
	}
	if len(pending) == 0 {
		return failed
	}
	if err := r.commitInlineImages(ctx, conversationKey, messageID, pending); err != nil {
		for _, image := range pending {
			r.itemFailed(ctx, entityInlineImage, image.id, conversationKey, err)
		}
		return failed + len(pending)
	}
	for _, image := range pending {
		if image.dataURI == "" {
			r.warn("inline_image_not_embedded", "An image is stored as a file on the message, but the message already holds 5 MiB of embedded images, so Show original keeps the Help Scout link for it.", conversationKey)
		}
	}
	return failed
}

func (r *run) downloadInlineImage(ctx context.Context, id string, ref imageRef, budget *int64) (*storedImage, error) {
	entry, found, err := r.ledger.get(ctx, entityInlineImage, id)
	if err != nil || (found && entry.Status == ledgerDone) {
		return nil, err
	}
	data, contentType, err := r.fetchInlineImage(ctx, ref.url)
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

func (r *run) fetchInlineImage(ctx context.Context, target string) ([]byte, string, error) {
	data, header, err := r.client.fetchImage(ctx, target, maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxImageBytes {
		return nil, "", fmt.Errorf("the image is over the %d byte limit for one image", maxImageBytes)
	}
	contentType, err := importkit.ImageType(header, data)
	return data, contentType, err
}
