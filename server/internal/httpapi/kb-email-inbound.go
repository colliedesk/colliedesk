package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// offersKBAddress reports whether workspaces get a knowledge base address: on the hosted cloud, with
// hosted addresses switched on, and only when a trusted receiving server stamps its verdict on mail,
// since a sender is proven by that stamp alone.
func (a *API) offersKBAddress() bool {
	config := a.config.Email
	return config.HostedMailDomain != "" && config.TrustedAuthServID != "" && !a.switchedOff(serverconfig.FeatureHostedMail)
}

// receiveKBEmail turns mail to the workspace's knowledge base address into a draft article. The sender
// must be proven: the trusted receiving server's stamp vouches for the From domain (DMARC pass, or DKIM
// pass for that domain), and From is an active member whose roles may write articles. From alone is
// written by the sender, and so is any Authentication-Results header the message carries; neither
// counts. Mail that fails is dropped with a 200, so the forwarder does not retry, and a warning names
// the reason without any address.
func (a *API) receiveKBEmail(response http.ResponseWriter, request *http.Request, workspaceID string, parsed email.Inbound, raw []byte) {
	ctx := request.Context()
	drop := func(reason string) {
		slog.Warn("knowledge base email dropped", "workspace", workspaceID, "reason", reason, "from_domain", email.Domain(parsed.FromAddress))
		writeJSON(response, http.StatusOK, store.InboundResult{Status: "dropped", Reason: &reason, Authentication: parsed.Authentication})
	}
	if !parsed.SenderAligned {
		drop("sender_not_verified")
		return
	}
	messageID := parsed.MessageID
	if messageID == "" {
		sum := sha256.Sum256(raw)
		messageID = "sha256:" + hex.EncodeToString(sum[:])
	}
	sender, err := a.store.KBEmailSenderOf(ctx, workspaceID, parsed.FromAddress, messageID, auth.PermKBArticlesCreate)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	switch {
	case sender.AgentID == "":
		drop("sender_not_member")
		return
	case !sender.Permitted:
		drop("sender_not_permitted")
		return
	case sender.ArticleID != "":
		writeKBEmailResult(response, parsed, sender.ArticleID, false)
		return
	case sender.CollectionID == "":
		drop("no_collection")
		return
	}
	body, err := kbEmailBody(parsed)
	if err != nil || domain.CheckKBBody(body) != nil {
		drop("invalid_content")
		return
	}
	draft := store.KBEmailDraft{MessageID: messageID,
		Article: store.KBArticleInput{CollectionID: sender.CollectionID, Title: kbEmailTitle(parsed.Subject), BodyMD: body, Status: domain.KBDraft}}
	if draft.Files, err = a.storeKBEmailFiles(ctx, workspaceID, parsed.Files); err != nil {
		a.discardKBEmailFiles(ctx, workspaceID, draft.Files)
		writeInternalError(response, err)
		return
	}
	result, err := a.store.CreateKBEmailDraft(ctx, workspaceID, store.Actor{AgentID: sender.AgentID}, draft)
	if err != nil {
		a.discardKBEmailFiles(ctx, workspaceID, draft.Files)
		if reason := kbEmailRefusal(err); reason != "" {
			drop(reason)
			return
		}
		writeInternalError(response, err)
		return
	}
	// The bytes a duplicate or a left-out file stored back nothing; DiscardObject keeps what a row uses.
	if !result.Created || len(result.Skipped) > 0 {
		a.discardKBEmailFiles(ctx, workspaceID, draft.Files)
	}
	if len(result.Skipped) > 0 {
		slog.Warn("knowledge base email files left out", "workspace", workspaceID, "article", result.ArticleID, "files", len(result.Skipped))
	}
	writeKBEmailResult(response, parsed, result.ArticleID, result.Created)
}

func writeKBEmailResult(response http.ResponseWriter, parsed email.Inbound, articleID string, created bool) {
	status := "duplicate"
	if created {
		status = "created"
	}
	writeJSON(response, http.StatusOK, store.InboundResult{Status: status, ArticleID: &articleID, Authentication: parsed.Authentication})
}

// kbEmailRefusal is the drop reason for a draft the knowledge base refuses, or "" for a failure the
// forwarder should retry.
func kbEmailRefusal(err error) string {
	var invalid *domain.InvalidKBError
	var limit *domain.PlanLimitError
	var storage *domain.StorageLimitError
	var locked *domain.LockedError
	switch {
	case errors.As(err, &invalid):
		return "invalid_content"
	case errors.As(err, &limit), errors.As(err, &storage), errors.As(err, &locked), errors.Is(err, domain.ErrKBArticleLimit), errors.Is(err, domain.ErrKBMediaLimit):
		return "plan_limit"
	case errors.Is(err, domain.ErrKBCollectionNotFound), errors.Is(err, domain.ErrKBCollectionInTrash):
		return "no_collection"
	}
	return ""
}

// replyPrefix is a Fwd:, Fw: or Re: a mail program puts before the subject it forwards or answers.
var replyPrefix = regexp.MustCompile(`(?i)^\s*(?:fwd?|re)\s*:\s*`)

// kbEmailTitle is the subject without its Fwd:, Fw: and Re: prefixes, cut to an article title.
func kbEmailTitle(subject string) string {
	title := strings.TrimSpace(subject)
	for {
		stripped := replyPrefix.ReplaceAllString(title, "")
		if stripped == title {
			break
		}
		title = strings.TrimSpace(stripped)
	}
	if title == "" {
		return "Forwarded mail"
	}
	if utf8.RuneCountInString(title) > domain.MaxKBArticleTitle {
		title = strings.TrimSpace(string([]rune(title)[:domain.MaxKBArticleTitle]))
	}
	return title
}

// kbEmailBody is the mail's text as Markdown: its HTML body without the head, style sheets and images
// (an inline image is part of the mail, not a file of the article), else its plain text.
func kbEmailBody(parsed email.Inbound) (string, error) {
	if strings.TrimSpace(parsed.BodyHTML) == "" {
		return strings.TrimSpace(parsed.Body), nil
	}
	document, err := html.Parse(strings.NewReader(parsed.BodyHTML))
	if err != nil {
		return "", err
	}
	body := findElement(document, atom.Body)
	if body == nil {
		return strings.TrimSpace(parsed.Body), nil
	}
	removeElements(body, atom.Img, atom.Style, atom.Title, atom.Head, atom.Script)
	var markup bytes.Buffer
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&markup, child); err != nil {
			return "", err
		}
	}
	markdown, _, err := freshdesk.ArticleMarkdown(markup.String())
	if err != nil {
		return "", err
	}
	if markdown == "" {
		return strings.TrimSpace(parsed.Body), nil
	}
	return markdown, nil
}

func findElement(node *html.Node, element atom.Atom) *html.Node {
	if node.Type == html.ElementNode && node.DataAtom == element {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, element); found != nil {
			return found
		}
	}
	return nil
}

func removeElements(node *html.Node, elements ...atom.Atom) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.ElementNode && slices.Contains(elements, child.DataAtom) {
			node.RemoveChild(child)
		} else {
			removeElements(child, elements...)
		}
		child = next
	}
}

// storeKBEmailFiles writes the mail's attachments that the media library takes to storage, with their
// type and searchable text. An inline image is part of the mail's text and is left out, as is a file
// whose name or bytes the library refuses; the most an article lists is kept.
func (a *API) storeKBEmailFiles(ctx context.Context, workspaceID string, files []email.File) ([]store.KBEmailFile, error) {
	var stored []store.KBEmailFile
	refused := 0
	for _, file := range files {
		if file.Inline || file.Embedded {
			continue
		}
		size := int64(len(file.Data))
		if len(stored) >= domain.MaxKBAttachments || size == 0 || size > domain.MaxKBFileBytes || media.CheckFileName(file.Filename) != nil {
			refused++
			continue
		}
		contentType, err := media.InspectFile(file.Filename, file.Data[:min(len(file.Data), kbFileHead)])
		if err != nil {
			refused++
			continue
		}
		object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(file.Data), size)
		if err != nil {
			return stored, err
		}
		stored = append(stored, store.KBEmailFile{Name: file.Filename, ContentType: contentType, Object: object,
			Text: media.FileText(contentType, bytes.NewReader(file.Data), size)})
	}
	if refused > 0 {
		slog.Warn("knowledge base email files left out", "workspace", workspaceID, "files", refused)
	}
	return stored, nil
}

// discardKBEmailFiles deletes the stored bytes no row of the workspace uses.
func (a *API) discardKBEmailFiles(ctx context.Context, workspaceID string, files []store.KBEmailFile) {
	for _, file := range files {
		if err := a.store.DiscardObject(ctx, workspaceID, file.Object.Key); err != nil {
			slog.Warn("knowledge base email: discard unrecorded bytes", "workspace", workspaceID, "error", err)
		}
	}
}
