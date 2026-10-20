package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of the media library.
const (
	// MaxKBMedia is the most library items a workspace holds, its trash included; the library's
	// latency gate holds its pages to budget at this cap.
	MaxKBMedia     = 10000
	MaxKBMediaName = 200
	// MaxKBMediaAlt bounds an image's alt text or a video's title.
	MaxKBMediaAlt = 500
	// MaxKBMediaPage is the most library items one page lists.
	MaxKBMediaPage = 100
	// MaxKBMediaBatch is the most ids one trash, restore or purge call names; an agent request names
	// them all, so a person approves exactly what will move (100 ids are about 4 KB of JSON).
	MaxKBMediaBatch = 100
	// MaxKBMediaUnusedBatch is the most unused items one "move all unused to the trash" call moves.
	MaxKBMediaUnusedBatch = 500
)

// Kinds of media.
const (
	KBMediaImage = "image"
	KBMediaVideo = "video"
)

// KBMediaPath is the API path of a library item; KBImagePath is the older path of an image, which
// article texts keep using.
const KBMediaPath = "/api/v1/kb/media/"

// KBMedia is an image or video of the knowledge base. URL is the path an article links. Width and
// Height are the size shown (a quarter-turned photo has them swapped); they are null until the
// server read them. Usage and PurgeAt are set by the library list and details.
type KBMedia struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	ContentType  string        `json:"content_type"`
	Size         int64         `json:"size"`
	SHA256       string        `json:"sha256"`
	URL          string        `json:"url"`
	Name         string        `json:"name"`
	Alt          string        `json:"alt"`
	Width        *int          `json:"width"`
	Height       *int          `json:"height"`
	DurationMS   *int64        `json:"duration_ms"`
	Codecs       []string      `json:"codecs"`
	ThumbnailURL *string       `json:"thumbnail_url"`
	Kept         bool          `json:"kept"`
	DeletedAt    *time.Time    `json:"deleted_at"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Usage        *KBMediaUsage `json:"usage,omitempty"`
	// PurgeAt is when the daily purge deletes an item no article ever saved and nobody kept.
	PurgeAt *time.Time `json:"purge_at,omitempty"`
}

// KBImage is an image of the library, as the image upload answers it.
type KBImage = KBMedia

// KBMediaUsage counts the articles that name the media: Articles in their current text (live
// articles, which make it "in use"), TrashedArticles in the current text of an article in the
// trash, OlderVersions only in an older version of an article.
type KBMediaUsage struct {
	Articles        int `json:"articles"`
	TrashedArticles int `json:"trashed_articles"`
	OlderVersions   int `json:"older_versions"`
}

// KBMediaArticle is an article that names the media, for its details.
type KBMediaArticle struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// InTrash is true for an article in the trash; Current is false when only an older version names it.
	InTrash bool `json:"in_trash"`
	Current bool `json:"current"`
}

// KBMediaDetails is a library item with the articles that name it.
type KBMediaDetails struct {
	KBMedia
	Articles []KBMediaArticle `json:"articles"`
}

// KBMediaSummary adds up the items of a library list that no live article uses: their count and
// the bytes deleting them frees, kept items apart.
type KBMediaSummary struct {
	Count     int   `json:"count"`
	Bytes     int64 `json:"bytes"`
	KeptCount int   `json:"kept_count"`
	KeptBytes int64 `json:"kept_bytes"`
}

// KBMediaFilter selects library items. Kind is empty, image or video; Usage is empty, in_use,
// unused or missing_alt; Sort is newest (default), oldest, name or largest.
type KBMediaFilter struct {
	Kind, Query, Usage, Sort, Cursor string
	Limit                            int
}

// KBMediaPatch changes a library item. Changing the name or alt keeps the item unless Kept says
// otherwise. UpdatedAt is what the client last read.
type KBMediaPatch struct {
	UpdatedAt time.Time
	Name      *string
	Alt       *string
	Kept      *bool
}

// KBImageUpload is what an image upload asks besides the bytes: a name and alt text for the library,
// and Keep for an upload made in the library, which the purge then never deletes.
type KBImageUpload struct {
	Name, Alt string
	Keep      bool
}

// What a trash, restore or purge call did to each id.
const (
	KBMediaTrashed        = "trashed"
	KBMediaAlreadyInTrash = "already_in_trash"
	KBMediaInUse          = "in_use"
	KBMediaNotFound       = "not_found"
	KBMediaRestored       = "restored"
	KBMediaNotInTrash     = "not_in_trash"
	KBMediaPurged         = "purged"
)

// KBMediaResult is what a call on several items did to one of them.
type KBMediaResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

var (
	ErrKBMediaNotFound      = errors.New("knowledge base media not found")
	ErrKBMediaBatchNotFound = errors.New("knowledge base media batch not found")
	ErrKBMediaLimit         = fmt.Errorf("a workspace holds at most %d images and videos in its media library, counting the trash; delete some first", MaxKBMedia)
)

// CheckKBMediaFilter refuses an unknown kind, usage or sort.
func CheckKBMediaFilter(filter KBMediaFilter) error {
	switch {
	case filter.Kind != "" && filter.Kind != KBMediaImage && filter.Kind != KBMediaVideo:
		return invalidKB("kind is image or video.")
	case filter.Usage != "" && filter.Usage != "in_use" && filter.Usage != "unused" && filter.Usage != "missing_alt":
		return invalidKB("usage is in_use, unused or missing_alt.")
	case filter.Sort != "" && filter.Sort != "newest" && filter.Sort != "oldest" && filter.Sort != "name" && filter.Sort != "largest":
		return invalidKB("sort is newest, oldest, name or largest.")
	}
	return nil
}

// CleanKBMediaText trims a library name or alt text and checks its length; empty is allowed.
func CleanKBMediaText(what, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > limit {
		return "", invalidKB("The %s holds at most %d characters.", what, limit)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", invalidKB("The %s is one line of text.", what)
	}
	return value, nil
}

// KBMediaIDs returns the media ids an article text links, each once and in lowercase: images through
// KBImagePath, any item (and a video's poster) through KBMediaPath.
func KBMediaIDs(body string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, match := range kbMediaLink.FindAllStringSubmatch(body, -1) {
		id := strings.ToLower(match[1])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

var kbMediaLink = regexp.MustCompile(`/api/v1/kb/(?:images|media)/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// KBMediaReplacement is one replacement of a library item in the articles that showed it: the
// articles that got a version naming To instead of From, and the translations rewritten, so it can be
// undone.
type KBMediaReplacement struct {
	ID           string                       `json:"id"`
	From         string                       `json:"from_media_id"`
	To           string                       `json:"to_media_id"`
	ArticleIDs   []string                     `json:"article_ids"`
	Translations []KBMediaReplacedTranslation `json:"translations"`
	// SkippedArticleIDs are the articles an undo left alone because they changed since the replacement.
	SkippedArticleIDs []string   `json:"skipped_article_ids"`
	CreatedAt         time.Time  `json:"created_at"`
	UndoneAt          *time.Time `json:"undone_at"`
}

// KBMediaReplacedTranslation is a translation a replacement rewrote.
type KBMediaReplacedTranslation struct {
	ArticleID string `json:"article_id"`
	Locale    string `json:"locale"`
}

var (
	ErrKBMediaReplacementNotFound = errors.New("knowledge base media replacement not found")
	// ErrKBMediaKindMismatch means a replacement of an image by a video or the other way.
	ErrKBMediaKindMismatch = errors.New("an image is replaced by an image, a video by a video")
	// ErrKBMediaGone means the item a replacement would bring back was deleted for good.
	ErrKBMediaGone = errors.New("the replaced item was deleted for good, so the replacement cannot be undone")
)

// ReplaceKBMediaIDs rewrites every link of the item from in text to the item to: image and media paths,
// and a video's poster path, matching the id in any letter case.
func ReplaceKBMediaIDs(text, from, to string) string {
	return kbMediaLink.ReplaceAllStringFunc(text, func(link string) string {
		match := kbMediaLink.FindStringSubmatch(link)
		if strings.EqualFold(match[1], from) {
			return link[:len(link)-len(match[1])] + to
		}
		return link
	})
}
