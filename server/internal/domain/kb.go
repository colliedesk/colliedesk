package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of the knowledge base. They count characters, not bytes. An article body is never cut: a
// longer one is refused, so nothing is lost silently.
const (
	MaxKBArticles       = 10000
	MaxKBCollections    = 1000
	MaxKBArticleTitle   = 300
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
	Title         string           `json:"title"`
	Slug          string           `json:"slug"`
	Status        string           `json:"status"`
	PublishedAt   *time.Time       `json:"published_at"`
	AuthorAgentID *string          `json:"author_agent_id"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	LastChange    *KBArticleChange `json:"last_change"`
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
	BodyMD        string                 `json:"body_md"`
	HasSourceHTML bool                   `json:"has_source_html"`
	Translations  []KBArticleTranslation `json:"translations"`
	DeletedAt     *time.Time             `json:"deleted_at"`
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
	Title        string  `json:"title"`
	Slug         string  `json:"slug"`
	BodyMD       string  `json:"body_md"`
	Status       string  `json:"status"`
	CollectionID *string `json:"collection_id"`
}

// KBImage is an image stored for articles; URL is the path an article links.
type KBImage struct {
	ID          string    `json:"id"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
}

// HelpCenter is the public help center's settings. UpdatedAt is nil until the first write.
type HelpCenter struct {
	Enabled        bool       `json:"enabled"`
	Title          string     `json:"title"`
	Locale         string     `json:"locale"`
	ContentVersion int64      `json:"content_version"`
	UpdatedAt      *time.Time `json:"updated_at"`
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

// Slugify makes a slug from a name: lowercase letters and digits of any script, other runs as one
// hyphen, at most MaxKBSlug characters. A name with no letter or digit gives fallback.
func Slugify(name, fallback string) string {
	var out strings.Builder
	hyphen := false
	count := 0
	for _, char := range strings.ToLower(name) {
		if count >= MaxKBSlug {
			break
		}
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
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

// KBImageIDs returns the image ids an article text links through KBImagePath, each once.
func KBImageIDs(body string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, match := range kbImageLink.FindAllStringSubmatch(body, -1) {
		id := strings.ToLower(match[1])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

var kbImageLink = regexp.MustCompile(regexp.QuoteMeta(KBImagePath) + `([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)
