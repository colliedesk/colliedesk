package httpapi

import (
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbExportRows turns articles into the cells of the knowledge base CSV.
type kbExportRows struct {
	home     string
	paths    map[string]string
	location *time.Location
}

func (r kbExportRows) row(item domain.KBArticleSummary, votes domain.KBVoteTally) []string {
	addressKBArticle(r.home, &item)
	author := ""
	if item.Author != nil {
		author = item.Author.Name
		if item.Author.Left {
			author += " (left)"
		}
	}
	languages := make([]string, len(item.Languages))
	for index, language := range item.Languages {
		languages[index] = language.Locale
		if language.Outdated {
			languages[index] += " (outdated)"
		}
	}
	cells := []string{item.Title, item.URL, r.paths[item.CollectionID], kbExportStatus(item), author,
		strings.Join(item.Tags, ", "), strings.Join(languages, ", "),
		item.CreatedAt.In(r.location).Format(time.RFC3339), item.UpdatedAt.In(r.location).Format(time.RFC3339),
		strconv.Itoa(votes.Helpful), strconv.Itoa(votes.NotHelpful)}
	for index, cell := range cells {
		cells[index] = spreadsheetText(cell)
	}
	return cells
}

// kbExportStatus is the one status the list shows: published, with unpublished changes, or a draft
// that waits for a reviewer, was approved, or neither.
func kbExportStatus(item domain.KBArticleSummary) string {
	switch {
	case item.Status == domain.KBPublished && item.HasDraft:
		return "unpublished_changes"
	case item.Status == domain.KBPublished:
		return domain.KBPublished
	case item.ReviewState != nil:
		return *item.ReviewState
	}
	return item.Status
}

// spreadsheetText keeps a cell that starts like a formula as text: a spreadsheet opening the file can
// run a cell starting with =, +, -, @, a tab or a carriage return, so such a cell starts with ' instead.
func spreadsheetText(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

// kbCollectionPaths names each collection with the collections above it, from the top: "Billing /
// Refunds".
func kbCollectionPaths(collections []domain.KBCollection) map[string]string {
	byID := make(map[string]domain.KBCollection, len(collections))
	for _, collection := range collections {
		byID[collection.ID] = collection
	}
	paths := make(map[string]string, len(collections))
	for _, collection := range collections {
		names := []string{collection.Name}
		for parent, depth := collection.ParentID, 0; parent != nil && depth < len(collections); depth++ {
			above, found := byID[*parent]
			if !found {
				break
			}
			names = append([]string{above.Name}, names...)
			parent = above.ParentID
		}
		paths[collection.ID] = strings.Join(names, " / ")
	}
	return paths
}
