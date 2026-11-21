package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// KBApprovalModes is what a collection says about approval: inherit (the workspace setting), on or off.
var KBApprovalModes = []string{"inherit", "on", "off"}

// The states of a review. Only an open review can be decided; an edit while it is open supersedes it.
const (
	KBReviewOpen             = "open"
	KBReviewApproved         = "approved"
	KBReviewChangesRequested = "changes_requested"
	KBReviewWithdrawn        = "withdrawn"
	KBReviewSuperseded       = "superseded"
)

// The version kinds that record a review in the article's or translation's history.
const (
	KBReviewRequested     = "review_request"
	KBReviewApprovedKind  = "review_approve"
	KBReviewChangesKind   = "review_changes"
	KBReviewWithdrawnKind = "review_withdraw"
	MaxKBReviewers        = 10
	MaxKBReviewNote       = 500
)

var (
	// ErrKBApprovalRequired means a collection of the article needs an approved version of exactly the
	// text being published.
	ErrKBApprovalRequired = errors.New("this collection needs an approved version; send it for review")
	// ErrKBTranslationApprovalRequired is ErrKBApprovalRequired for a translation or its unpublished
	// changes.
	ErrKBTranslationApprovalRequired = errors.New("this collection needs an approved version of the translation")
	ErrKBReviewNotFound              = errors.New("knowledge base review not found")
	// ErrKBNothingToReview means the article or translation has no unpublished text.
	ErrKBNothingToReview = errors.New("there is nothing unpublished to review: edit the text first")
	// ErrKBReviewClosed means the review was already decided, withdrawn or superseded by an edit.
	ErrKBReviewClosed = errors.New("this review is no longer open; read the article for the current one")
	// ErrKBReviewOpen means the text already has an open review.
	ErrKBReviewOpen = errors.New("this text already has an open review; withdraw it or wait for the decision")
	// ErrKBNotAReviewer means the caller was not asked to review it.
	ErrKBNotAReviewer = errors.New("only a person asked to review this text can decide it")
	// ErrKBOwnReview means the person who sent the text for review, or one of their tokens, tried to decide it.
	ErrKBOwnReview = errors.New("the person who sent a text for review, and their agent tokens, cannot approve it; another reviewer must")
)

// KBReviewer is a person asked to review a text and whether they opened the review.
type KBReviewer struct {
	AgentID string     `json:"agent_id"`
	Name    string     `json:"name"`
	SeenAt  *time.Time `json:"seen_at"`
}

// KBReview is one review of one text: the article's own (Locale nil) or a translation's. ContentHash is
// the hash of the text it was asked about; an approval holds only while the text still has it.
type KBReview struct {
	ID                 string            `json:"id"`
	ArticleID          string            `json:"article_id"`
	ArticleTitle       string            `json:"article_title"`
	Locale             *string           `json:"locale"`
	State              string            `json:"state"`
	Note               string            `json:"note"`
	ContentHash        string            `json:"content_hash"`
	Current            bool              `json:"current"`
	Reviewers          []KBReviewer      `json:"reviewers"`
	RequestedBy        *SavedReplyPerson `json:"requested_by"`
	RequestedByToken   *SavedReplyToken  `json:"requested_by_token"`
	DecidedBy          *SavedReplyPerson `json:"decided_by"`
	DecidedByToken     *SavedReplyToken  `json:"decided_by_token"`
	DecidedAt          *time.Time        `json:"decided_at"`
	DecisionNote       string            `json:"decision_note"`
	SupersedesReviewID *string           `json:"supersedes_review_id"`
	CreatedAt          time.Time         `json:"created_at"`
}

// KBContent is what a review approves: the fields the help center shows, where it lists the text and
// the files it lists under it.
type KBContent struct {
	Title          string        `json:"title"`
	Slug           string        `json:"slug"`
	BodyMD         string        `json:"body_md"`
	SEOTitle       string        `json:"seo_title"`
	SEODescription string        `json:"seo_description"`
	CollectionIDs  []string      `json:"collection_ids"`
	Attachments    KBAttachments `json:"attachments"`
	// Tags are the article's, cleaned (CleanKBTags); a translation's text leaves them out.
	Tags []string `json:"tags,omitempty"`
}

// KBReviewDetail is a review with the text it is about: Proposed is the text waiting to be published
// now (the reviewed one while Current), Live what the help center shows (nil while nothing is
// published), and Diff the lines of the body from Live to Proposed.
type KBReviewDetail struct {
	KBReview
	Live     *KBContent `json:"live"`
	Proposed KBContent  `json:"proposed"`
	Diff     []DiffLine `json:"diff"`
}

// KBReviewFilter narrows a list of reviews: a state, a reviewer's agent id, an article.
type KBReviewFilter struct {
	State      string
	ReviewerID string
	ArticleID  string
	// Locale narrows an article's reviews to those of its translation; nil keeps the article's own.
	Locale *string
}

// Hash is the content hash of c: sha256 over a JSON array of its fields, the collections sorted and the
// files' ids in their order, so no two different texts share one. A text without files hashes as it did
// before texts listed files.
func (c KBContent) Hash() string {
	collections := slices.Clone(c.CollectionIDs)
	slices.Sort(collections)
	collections = slices.Compact(collections)
	fields := []any{c.Title, c.Slug, c.BodyMD, c.SEOTitle, c.SEODescription, collections}
	if len(c.Attachments) > 0 {
		files := make([]string, len(c.Attachments))
		for index, file := range c.Attachments {
			files[index] = file.ID
		}
		fields = append(fields, files)
	}
	if len(c.Tags) > 0 {
		tags := slices.Clone(c.Tags)
		slices.Sort(tags)
		fields = append(fields, map[string][]string{"tags": slices.Compact(tags)})
	}
	encoded, _ := json.Marshal(fields)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// Content is the article's live text as a review sees it.
func (a KBArticle) Content() KBContent {
	return KBContent{Title: a.Title, Slug: a.Slug, BodyMD: a.BodyMD, SEOTitle: a.SEOTitle, SEODescription: a.SEODescription, CollectionIDs: a.CollectionIDs,
		Attachments: a.Attachments, Tags: a.Tags}
}
