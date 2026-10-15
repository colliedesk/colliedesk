package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A canned response becomes a saved reply titled "<folder> / <title>", so the folder a response lived
// in is still visible and two folders can hold the same title.
const folderSeparator = " / "

var (
	placeholderPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	blankSpaceLines    = regexp.MustCompile(`(?m)^[ \t]+$`)
)

// mappedPlaceholders are the Freshdesk placeholders that mean the same as a Colliedesk variable.
var mappedPlaceholders = map[string]string{
	"ticket.requester.name": "customer.name",
	"ticket.from_name":      "customer.name",
	"ticket.agent.name":     "agent.name",
}

// mapPlaceholders rewrites the placeholders Colliedesk has an equivalent for and returns the names of
// the others, which stay in the text as written.
func mapPlaceholders(body string) (string, []string) {
	var unmapped []string
	mapped := placeholderPattern.ReplaceAllStringFunc(body, func(match string) string {
		name := strings.TrimSpace(match[2 : len(match)-2])
		if replacement, ok := mappedPlaceholders[name]; ok {
			return "{{" + replacement + "}}"
		}
		if name != "" {
			unmapped = append(unmapped, name)
		}
		return match
	})
	return mapped, unmapped
}

// cannedText is the plain text of a canned response: Freshdesk's own plain-text version, or the HTML
// version reduced to text the way inbound mail is when there is none.
func cannedText(response fdCannedResponse) string {
	text := response.Content
	if strings.TrimSpace(text) == "" && strings.TrimSpace(response.ContentHTML) != "" {
		text = email.HTMLToText(keepLinks(response.ContentHTML))
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.TrimSpace(blankSpaceLines.ReplaceAllString(text, ""))
}

var (
	anchorPattern = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagPattern    = regexp.MustCompile(`(?s)<[^>]*>`)
	imagePattern  = regexp.MustCompile(`(?i)<img\b`)
)

// keepLinks writes <a href="url">text</a> as "text (url)", so the address survives the reduction to
// text. A link whose text already is the address stays as that address.
func keepLinks(markup string) string {
	return anchorPattern.ReplaceAllStringFunc(markup, func(match string) string {
		parts := anchorPattern.FindStringSubmatch(match)
		url, label := strings.TrimSpace(parts[1]), strings.TrimSpace(tagPattern.ReplaceAllString(parts[2], ""))
		switch {
		case label == "" || label == url || "mailto:"+label == url:
			return url
		case strings.HasPrefix(url, "#") || strings.HasPrefix(strings.ToLower(url), "javascript:"):
			return label
		}
		return label + " (" + url + ")"
	})
}

func savedReplyTitle(folder, title string) string {
	folder, title = strings.TrimSpace(folder), strings.TrimSpace(title)
	if folder != "" && title != "" {
		return folder + folderSeparator + title
	}
	return folder + title
}

// fitTitle keeps a title within the saved reply limit, cutting the part before the suffix.
func fitTitle(title, suffix string) string {
	keep := domain.MaxSavedReplyTitle - utf8.RuneCountInString(suffix)
	if runes := []rune(title); len(runes) > keep {
		title = strings.TrimSpace(string(runes[:keep]))
	}
	return title + suffix
}

// importSavedReplies lists the canned response folders and imports their responses. Item problems
// go to the ledger and the report. Only cancellation and ledger errors stop the run: a folder list
// that cannot be read is reported as a failure and the tickets still import.
func (r *run) importSavedReplies(ctx context.Context) error {
	verification := Verification{Verified: true,
		Method: "The sum of responses_count over the canned response folders, compared with the canned responses listed in them."}
	var folders []fdCannedFolder
	err := listAll(ctx, r.client, r.endpoint("/api/v2/canned_response_folders", pageQuery()), func(folder fdCannedFolder) error {
		folders = append(folders, folder)
		return nil
	})
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errNotFound) || errors.Is(err, errForbidden):
		r.warn("saved_replies_unavailable", "Freshdesk refused the canned response folders, so canned responses were not imported. Use an admin API key on a plan that has canned responses, then run the import again.", "")
		verification.Verified, verification.Detail = false, "The canned response folders could not be read: "+err.Error()
		r.verification.SavedReplies = verification
		return nil
	case err != nil:
		r.cannedListError = "list canned response folders: " + err.Error()
		verification.Verified, verification.Detail = false, "The canned response folders could not be listed: "+err.Error()
		r.verification.SavedReplies = verification
		return nil
	}
	r.cannedFolders = len(folders)
	reported, listed := 0, 0
	for _, folder := range folders {
		id := strconv.FormatInt(folder.ID, 10)
		reported += folder.ResponsesCount
		if folder.Personal && r.opts.IncludePersonalCannedResponses {
			r.warn("saved_reply_personal_folder", "This Freshdesk folder is personal. Its canned responses were imported and are now shared with the whole workspace; delete any that should stay private.", id)
		}
		if folder.Personal && !r.opts.IncludePersonalCannedResponses {
			r.warn("saved_reply_personal_folder", "This Freshdesk folder is personal, so its canned responses were not imported. Run again with --include-personal-canned-responses to share them with the workspace.", id)
		}
		var responses []fdCannedResponse
		err := listAll(ctx, r.client, r.endpoint("/api/v2/canned_response_folders/"+id+"/responses", pageQuery()), func(response fdCannedResponse) error {
			responses = append(responses, response)
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			r.cannedUnreadable = append(r.cannedUnreadable, id)
			if !r.opts.DryRun {
				r.itemFailed(ctx, entityCannedFolder, id, "", fmt.Errorf("read canned responses: %w", err))
			}
			continue
		}
		if !r.opts.DryRun {
			if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entityCannedFolder, sourceID: id, status: ledgerDone}); err != nil {
				return err
			}
		}
		listed += len(responses)
		for _, response := range responses {
			if folder.Personal && !r.opts.IncludePersonalCannedResponses {
				r.cannedPersonal = append(r.cannedPersonal, strconv.FormatInt(response.ID, 10))
				continue
			}
			r.importSavedReply(ctx, folder, response)
		}
	}
	switch {
	case len(r.cannedUnreadable) > 0:
		verification.Verified = false
		verification.Detail = fmt.Sprintf("The canned responses of folder(s) %s could not be read.", strings.Join(r.cannedUnreadable, ", "))
	case reported != listed:
		verification.Verified = false
		verification.Detail = fmt.Sprintf("Freshdesk folders report %d canned responses, but %d were listed.", reported, listed)
	default:
		verification.Detail = fmt.Sprintf("Freshdesk folders report %d canned responses; %d were listed.", reported, listed)
	}
	r.verification.SavedReplies = verification
	return nil
}

func (r *run) importSavedReply(ctx context.Context, folder fdCannedFolder, response fdCannedResponse) {
	key := strconv.FormatInt(response.ID, 10)
	if len(response.Attachments) > 0 {
		r.cannedAttachments += len(response.Attachments)
		r.cannedWithFiles = append(r.cannedWithFiles, key)
		r.warn("saved_reply_attachments_not_imported", "Saved replies hold text only, so the files on this canned response were not imported. Download them from Freshdesk if they are still needed.", key)
	}
	if imagePattern.MatchString(response.ContentHTML) {
		r.warn("saved_reply_images_dropped", "The HTML of this canned response holds an image. Saved replies hold text only, so the image was dropped; add it by hand or link it.", key)
	}
	text := cannedText(response)
	if text == "" {
		r.cannedEmpty = append(r.cannedEmpty, key)
		r.warn("saved_reply_empty", "This canned response has no text, so there is nothing to import.", key)
		return
	}
	body, unmapped := mapPlaceholders(text)
	r.cannedIDs = append(r.cannedIDs, key)
	for _, name := range unmapped {
		r.cannedPlaceholders[name] = append(r.cannedPlaceholders[name], key)
	}
	if len(unmapped) > 0 {
		r.warn("saved_reply_placeholder_not_mapped", "This canned response holds a Freshdesk placeholder that Colliedesk has no variable for. It stays in the text as written; edit the saved reply before using it. The names are in saved_replies.unmapped_placeholders.", key)
	}
	if r.opts.DryRun {
		return
	}
	if err := r.writeSavedReply(ctx, key, savedReplyTitle(folder.Name, response.Title), body); err != nil {
		r.itemFailed(ctx, entitySavedReply, key, "", err)
	}
}

// writeSavedReply adds the saved reply once. A reply already imported is left alone, so an agent's
// edit or deletion survives a rerun. A title taken by another saved reply gets the Freshdesk id appended.
func (r *run) writeSavedReply(ctx context.Context, key, title, body string) error {
	if _, ok, err := r.ledger.done(ctx, entitySavedReply, key); err != nil || ok {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin saved reply import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	reply, err := store.InsertSavedReply(ctx, tx, r.workspaceID, nil, fitTitle(title, ""), body)
	if errors.Is(err, domain.ErrDuplicateTitle) {
		reply, err = store.InsertSavedReply(ctx, tx, r.workspaceID, nil, fitTitle(title, " (Freshdesk "+key+")"), body)
	}
	if err != nil {
		return err
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entitySavedReply, sourceID: key, targetID: &reply.ID, status: ledgerDone}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit saved reply import: %w", err)
	}
	return nil
}
