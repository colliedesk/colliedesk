package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Limits of the knowledge base. They count characters, not bytes. An article body is never cut: a
// longer one is refused, so nothing is lost silently. MaxKBArticles is the one place the article cap
// lives: a search reads every article of the workspace (no index under row-level security), and
// TestKnowledgeBaseSearchLatencyBudget holds it to 100 ms at this cap. Raise both together, and read
// docs/internals/knowledge-base.md first.
const (
	MaxKBArticles     = 2000
	MaxKBCollections  = 1000
	MaxKBArticleTitle = 300
	// MaxKBSEOTitle and MaxKBSEODescription bound an article's own title and description for search
	// engines (the database checks them too).
	MaxKBSEOTitle       = 120
	MaxKBSEODescription = 320
	MaxKBArticleBody    = 100000
	MaxKBCollectionName = 200
	MaxKBDescription    = 2000
	MaxKBSlug           = 120
	MaxKBPosition       = 1000000
	// MaxKBDepth is how deep collections nest: a Freshdesk category, folder and subfolder.
	MaxKBDepth = 3
	// MaxKBImageBytes bounds one image.
	MaxKBImageBytes = 5 << 20
	// KBTrashRetention is how long a deleted article or collection stays restorable, and how old an
	// image no article links must be before the purge deletes it.
	KBTrashRetention   = 30 * 24 * time.Hour
	MaxHelpCenterTitle = 200
)

// Kinds of article versions.
const (
	KBArticleCreated   = "create"
	KBArticleEdited    = "edit"
	KBArticleDeleted   = "delete"
	KBArticleRestored  = "restore"
	KBArticleUndeleted = "undelete"
	KBArticleImported  = "import"
	// KBArticleMediaReplaced: a library item was replaced in the text, or the replacement undone.
	KBArticleMediaReplaced = "media_replace"
	// KBArticleAIExcluded, KBArticleAIIncluded: AI was kept away from the article, or let read it again.
	KBArticleAIExcluded = "ai_exclude"
	KBArticleAIIncluded = "ai_include"
)

// Article statuses.
const (
	KBDraft     = "draft"
	KBPublished = "published"
)

// KBArticleChange is who made one version of an article, and how; it has the shape of a saved reply change.
type KBArticleChange = SavedReplyChange

// KBCollection groups articles; ParentID nests it, at most MaxKBDepth levels deep.
type KBCollection struct {
	ID           string    `json:"id"`
	ParentID     *string   `json:"parent_id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Description  string    `json:"description"`
	Position     int       `json:"position"`
	ArticleCount int       `json:"article_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TrashedKBCollection is a deleted collection, restorable until PurgesAt.
type TrashedKBCollection struct {
	KBCollection
	DeletedAt time.Time         `json:"deleted_at"`
	DeletedBy *SavedReplyPerson `json:"deleted_by"`
	PurgesAt  time.Time         `json:"purges_at"`
}

// KBArticleSummary is an article without its text, as lists show it.
type KBArticleSummary struct {
	ID            string           `json:"id"`
	Number        int64            `json:"number"`
	CollectionID  string           `json:"collection_id"`
	Position      int              `json:"position"`
	Title         string           `json:"title"`
	Slug          string           `json:"slug"`
	Status        string           `json:"status"`
	PublishedAt   *time.Time       `json:"published_at"`
	AuthorAgentID *string          `json:"author_agent_id"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	LastChange    *KBArticleChange `json:"last_change"`
	// URL is the public address on the help center; the API fills it in, the store does not know it.
	URL string `json:"url"`
}

// KBArticleTranslation is a translation an import brought in; it is kept and exported, not rendered yet.
type KBArticleTranslation struct {
	Locale    string    `json:"locale"`
	Title     string    `json:"title"`
	BodyMD    string    `json:"body_md"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KBArticle is an article with its Markdown text.
type KBArticle struct {
	KBArticleSummary
	BodyMD string `json:"body_md"`
	// SEOTitle and SEODescription are what search engines show; empty uses the title and the start
	// of the text.
	SEOTitle       string                 `json:"seo_title"`
	SEODescription string                 `json:"seo_description"`
	HasSourceHTML  bool                   `json:"has_source_html"`
	Translations   []KBArticleTranslation `json:"translations"`
	DeletedAt      *time.Time             `json:"deleted_at"`
	// AIExclusion is set while AI may not read the article, in any language.
	AIExclusion *KBAIExclusion `json:"ai_exclusion"`
	// MissingMedia is set only in the answer to a save: media ids the text names that the library
	// does not have.
	MissingMedia []string `json:"missing_media,omitempty"`
	// AIDraftsThisMonth is set only when one article is read: the AI reply drafts this UTC month that
	// used it as a source.
	AIDraftsThisMonth *int `json:"ai_drafts_this_month,omitempty"`
}

// KBAIExclusion is since when, and by whom, an article is kept away from AI.
type KBAIExclusion struct {
	At time.Time         `json:"at"`
	By *SavedReplyPerson `json:"by"`
}

// TrashedKBArticle is a deleted article, restorable until PurgesAt.
type TrashedKBArticle struct {
	KBArticleSummary
	DeletedAt time.Time         `json:"deleted_at"`
	DeletedBy *SavedReplyPerson `json:"deleted_by"`
	PurgesAt  time.Time         `json:"purges_at"`
}

// KBArticleVersion is one version of an article with what it said after the change.
type KBArticleVersion struct {
	KBArticleChange
	Title          string  `json:"title"`
	Slug           string  `json:"slug"`
	BodyMD         string  `json:"body_md"`
	SEOTitle       string  `json:"seo_title"`
	SEODescription string  `json:"seo_description"`
	Status         string  `json:"status"`
	CollectionID   *string `json:"collection_id"`
}

// HelpCenter is the public help center's settings. UpdatedAt is nil until the first write.
type HelpCenter struct {
	Enabled        bool       `json:"enabled"`
	Title          string     `json:"title"`
	Locale         string     `json:"locale"`
	ContentVersion int64      `json:"content_version"`
	UpdatedAt      *time.Time `json:"updated_at"`
	// URL is the public address; the API sets it, the store leaves it empty.
	URL string `json:"url"`
}

// KBImagePath is the API path of an image; articles link images by it.
const KBImagePath = "/api/v1/kb/images/"

var (
	ErrKBArticleNotFound        = errors.New("knowledge base article not found")
	ErrKBArticleVersionNotFound = errors.New("knowledge base article version not found")
	ErrKBCollectionNotFound     = errors.New("knowledge base collection not found")
	ErrKBImageNotFound          = errors.New("knowledge base image not found")
	// ErrKBChanged means the row was saved by someone else after the client read it.
	ErrKBChanged = errors.New("this was changed by someone else; reload to see their version")
	// ErrKBDuplicateSlug means a sibling collection outside the trash has the slug.
	ErrKBDuplicateSlug = errors.New("another collection in the same place has this slug")
	// ErrKBCollectionNotEmpty means the collection still holds articles or collections outside the trash.
	ErrKBCollectionNotEmpty = errors.New("this collection still holds articles or collections; move or delete them first")
	// ErrKBParentInTrash means the parent collection is in the trash.
	ErrKBParentInTrash = errors.New("the parent collection is in the trash; restore it first")
	// ErrKBCollectionInTrash means the article's collection is in the trash.
	ErrKBCollectionInTrash = errors.New("the article's collection is in the trash; restore it first")
	ErrKBArticleLimit      = fmt.Errorf("a workspace holds at most %d knowledge base articles, counting the trash; delete one first", MaxKBArticles)
	ErrKBCollectionLimit   = fmt.Errorf("a workspace holds at most %d knowledge base collections, counting the trash; delete one first", MaxKBCollections)
)

// InvalidKBError says which part of a knowledge base write the API refuses.
type InvalidKBError struct{ Message string }

func (e *InvalidKBError) Error() string { return e.Message }

func invalidKB(format string, args ...any) error {
	return &InvalidKBError{fmt.Sprintf(format, args...)}
}

// CleanKBText trims a required name or title and checks its length.
func CleanKBText(what, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return "", invalidKB("The %s is empty.", what)
	case utf8.RuneCountInString(value) > limit:
		return "", invalidKB("The %s holds at most %d characters.", what, limit)
	}
	return value, nil
}

// CleanKBSEO is a search title or description as stored: on one line, runs of white space as one
// space, trimmed; empty is allowed (the default is used) and a longer one is refused, never cut.
func CleanKBSEO(what, value string, limit int) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		return "", invalidKB("The %s holds at most %d characters.", what, limit)
	}
	return value, nil
}

// CheckKBBody refuses a text longer than MaxKBArticleBody. It is stored as written, never cut.
func CheckKBBody(body string) error {
	if utf8.RuneCountInString(body) > MaxKBArticleBody {
		return invalidKB("The text holds at most %d characters; it was not cut, split it into several articles.", MaxKBArticleBody)
	}
	return nil
}

// CheckKBDescription refuses a description longer than MaxKBDescription.
func CheckKBDescription(description string) error {
	if utf8.RuneCountInString(description) > MaxKBDescription {
		return invalidKB("The description holds at most %d characters.", MaxKBDescription)
	}
	return nil
}

// CheckKBPosition refuses a position outside 0..MaxKBPosition.
func CheckKBPosition(position int) error {
	if position < 0 || position > MaxKBPosition {
		return invalidKB("position is a whole number from 0 to %d.", MaxKBPosition)
	}
	return nil
}

// CheckKBStatus refuses anything but draft and published.
func CheckKBStatus(status string) error {
	if status != KBDraft && status != KBPublished {
		return invalidKB("status is draft or published.")
	}
	return nil
}

// Slugify makes a slug from a name in NFC: lowercase letters, digits and combining marks of any
// script, other runs as one hyphen, at most MaxKBSlug characters. A name with no letter or digit gives fallback.
func Slugify(name, fallback string) string {
	var out strings.Builder
	hyphen := false
	count := 0
	for _, char := range strings.ToLower(norm.NFC.String(name)) {
		if count >= MaxKBSlug {
			break
		}
		// A combining mark (a vowel sign in Hindi or Thai, an accent typed apart) belongs to its word.
		if unicode.IsLetter(char) || unicode.IsNumber(char) || (unicode.IsMark(char) && out.Len() > 0 && !hyphen) {
			if hyphen && out.Len() > 0 {
				out.WriteByte('-')
				count++
			}
			out.WriteRune(char)
			count++
			hyphen = false
			continue
		}
		hyphen = true
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return fallback
	}
	return slug
}

// CheckKBSlug refuses a slug that Slugify would change: only lowercase letters and digits of any
// script joined by single hyphens.
func CheckKBSlug(slug string) error {
	if slug == "" || utf8.RuneCountInString(slug) > MaxKBSlug || Slugify(slug, "") != slug {
		return invalidKB("A slug is 1 to %d lowercase letters or digits joined by single hyphens, such as reset-your-password.", MaxKBSlug)
	}
	return nil
}

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)

// CheckLocale refuses what is not shaped like a BCP 47 language tag.
func CheckLocale(locale string) error {
	if len(locale) < 2 || len(locale) > 35 || !localePattern.MatchString(locale) {
		return invalidKB("locale is a BCP 47 language tag such as en or pt-BR.")
	}
	return nil
}

// HelpCenterSite is what every public help center page needs to know about its workspace. Trial is
// true while the workspace is on a trial: its pages ask search engines not to index them.
type HelpCenterSite struct {
	WorkspaceID    string
	Slug           string
	Name           string
	Title          string
	Locale         string
	ContentVersion int64
	Trial          bool
	// Theme is the brand the pages show. A logo or favicon that is no longer an image of the library
	// outside its trash is left out (nil), so a page never links an image it cannot serve.
	Theme HelpCenterTheme
	// LogoWidth and LogoHeight are the logo's size in pixels, 0 when unknown.
	LogoWidth, LogoHeight int
	// SearchConsole are the verification tokens the pages carry as <meta> tags.
	SearchConsole SearchConsole
	// CustomHost is the workspace's active custom domain, "" when it has none or is on its trial: the
	// help center serves there and its other addresses redirect there.
	CustomHost string
}

// PublicKBCollection is a live collection as the help center lists it; Published counts its own
// published articles, not those of the collections inside it.
type PublicKBCollection struct {
	ID          string
	ParentID    string
	Name        string
	Slug        string
	Description string
	Published   int
}

// PublicKBArticle is a published article as the help center shows it. BodyMD is empty in lists;
// Excerpt is the start of the text, set only by a search.
type PublicKBArticle struct {
	Number       int64
	Slug         string
	Title        string
	CollectionID string
	BodyMD       string
	Excerpt      string
	// SEOTitle and SEODescription are read with the article page only; empty uses the defaults.
	SEOTitle, SEODescription string
	PublishedAt              *time.Time
	UpdatedAt                time.Time
	// Videos are the library videos the text may show, read with the article page only.
	Videos []PublicKBVideo
}

// PublicKBVideo is a library video a published article's current text links, as its page shows it.
// PosterVersion is the end of the poster's content address ("" for none).
type PublicKBVideo struct {
	ID            string
	Width, Height int
	PosterVersion string
}
