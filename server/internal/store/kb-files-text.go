package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// aiFilesTextBytes caps the text of an article's files AI reads with the article, all files together.
// One file's text is at most 200 KB already; the cap keeps an article that lists many of them well below
// PostgreSQL's 1 MB limit of a tsvector and a help chat prompt of one article reasonable.
const aiFilesTextBytes = 200 << 10

// kbListedFile is a file an article lists, with the text read from it at upload ("" for none).
type kbListedFile struct{ id, name, sha256, text string }

// kbListedFiles are the files of articles outside the trash, by article id and list ("" is the
// article's own list, a locale its translation's), in their order.
type kbListedFiles map[string]map[string][]kbListedFile

// readKBListedFiles reads the lists of the articles ids in one query.
func readKBListedFiles(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (kbListedFiles, error) {
	files := kbListedFiles{}
	if len(ids) == 0 {
		return files, nil
	}
	rows, err := tx.Query(ctx, `SELECT l.article_id::text, COALESCE(l.locale,''), m.id::text, m.name, m.sha256, COALESCE(m.text,'')
		FROM kb_article_attachments l
		JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id AND m.kind='file' AND m.deleted_at IS NULL
		WHERE l.workspace_id=$1 AND l.article_id=ANY($2::uuid[]) ORDER BY l.article_id, l.locale NULLS FIRST, l.position`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read the files of articles: %w", err)
	}
	var article, locale string
	var file kbListedFile
	_, err = pgx.ForEachRow(rows, []any{&article, &locale, &file.id, &file.name, &file.sha256, &file.text}, func() error {
		if files[article] == nil {
			files[article] = map[string][]kbListedFile{}
		}
		files[article][locale] = append(files[article][locale], file)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the files of articles: %w", err)
	}
	return files, nil
}

// shown are the files the page of the article in language locale lists ("" the article's own text):
// the translation's own list, or the article's while the translation lists none (publicKBAttachments).
func (f kbListedFiles) shown(article, locale string) []kbListedFile {
	if own := f[article][locale]; locale != "" && len(own) > 0 {
		return own
	}
	return f[article][""]
}

var fileHeadingLine = strings.NewReplacer("\r", " ", "\n", " ")

// kbFilesText is the text of the files AI reads after the article's own, each file under a heading that
// names it, its type and the article that lists it ("## Warranty form (PDF) in Returns policy"), so an
// answer from it can say where the reader finds it. A file without text adds nothing; the text stops at
// aiFilesTextBytes.
func kbFilesText(title string, files []kbListedFile) string {
	var out strings.Builder
	for _, file := range files {
		text := strings.TrimSpace(file.text)
		if text == "" {
			continue
		}
		block := "\n\n## " + fileHeadingLine.Replace(domain.KBFileStem(file.name)) + " (" + domain.KBFileLabel(file.name) + ") in " +
			fileHeadingLine.Replace(title) + "\n\n" + text
		if room := aiFilesTextBytes - out.Len(); len(block) > room {
			block = block[:room]
			for !utf8.ValidString(block) {
				block = block[:len(block)-1]
			}
			out.WriteString(block)
			break
		}
		out.WriteString(block)
	}
	return out.String()
}

// kbFilesKey changes whenever a file of the list is added, removed, moved, renamed or replaced; ""
// for no file, so an article without files is named as before.
func kbFilesKey(files []kbListedFile) string {
	if len(files) == 0 {
		return ""
	}
	sum := sha256.New()
	for _, file := range files {
		fmt.Fprintf(sum, "%s\x00%s\x00%s\x01", file.id, file.sha256, file.name)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// withKBFiles adds to the help chat's articles the files their pages list: their bytes to Size, their
// key to FilesKey and, when the bodies were read, their text to Body.
func withKBFiles(ctx context.Context, tx pgx.Tx, workspaceID string, articles []ChatKBArticle, withBodies bool) error {
	ids := make([]string, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	files, err := readKBListedFiles(ctx, tx, workspaceID, ids)
	if err != nil {
		return err
	}
	for i := range articles {
		shown := files.shown(articles[i].ID, articles[i].Locale)
		text := kbFilesText(articles[i].Title, shown)
		articles[i].Size += len(text)
		articles[i].FilesKey = kbFilesKey(shown)
		if withBodies {
			articles[i].Body += text
		}
	}
	return nil
}
