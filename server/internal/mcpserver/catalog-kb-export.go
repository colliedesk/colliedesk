package mcpserver

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

// The article list as a spreadsheet, and the tags it filters by.

const (
	kbExportPath = "/api/v1/kb/articles/export.csv"
	// kbExportToolRows is the most articles one answer carries; the file at download_path has them all.
	kbExportToolRows = 5000
)

func kbArticlesExportSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_articles_export",
		Title: "Export knowledge base articles as CSV",
		Description: `Give the article list as the CSV file the list page downloads, with the same filters and order as colliedesk_kb_articles_search: one row per article with its title, address, collection, ` +
			`status, author, tags, languages, created and updated times in your timezone, and vote counts. A cell that starts with =, +, -, @, a tab or a carriage return starts with ' so a spreadsheet keeps it as text. ` +
			`At most limit rows (5,000); truncated says more match, and download_path is the whole file for a person signed in to Colliedesk.` + "\n\n" + kbTextNotice,
		REST: []string{"GET " + kbExportPath},
		Input: `{"type":"object","additionalProperties":false,"properties":{` + kbArticleListInput +
			`,"limit":{"type":"integer","minimum":1,"maximum":5000,"default":5000,"description":"The most rows in csv."}}}`,
		Output: `{"type":"object","required":["csv","rows","truncated","download_path","next"],"properties":{` +
			`"csv":{"type":"string","description":"UTF-8 CSV: the header row, then one row per article."},"rows":{"type":"integer","description":"Articles in csv."},` +
			`"truncated":{"type":"boolean","description":"More articles match than csv holds."},` +
			`"download_path":{"type":"string","description":"The whole file (up to 50,000 rows), a path on this Colliedesk."},"next":{"type":"string"}}}`,
		run: kbArticlesExport,
	}
}

func kbTagsListSpec() Spec {
	return Spec{
		Name:        "colliedesk_kb_tags_list",
		Title:       "List knowledge base tags",
		Description: `List every tag the articles outside the trash have published, by name, with how many articles have it. Pass names as tags to colliedesk_kb_articles_search or colliedesk_kb_articles_export.`,
		REST:        []string{"GET /api/v1/kb/tags"},
		Input:       `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: `{"type":"object","required":["tags"],"properties":{"tags":{"type":"array","items":{"type":"object","required":["tag","article_count"],` +
			`"properties":{"tag":{"type":"string"},"article_count":{"type":"integer"}}}}}}`,
		run: kbTagsList,
	}
}

func kbArticlesExport(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Limit int `json:"limit"`
		kbArticleListArguments
	}](arguments)
	if err != nil {
		return nil, err
	}
	limit := kbExportToolRows
	if args.Limit > 0 {
		limit = min(args.Limit, kbExportToolRows)
	}
	path := kbExportPath
	if encoded := args.listValues().Encode(); encoded != "" {
		path += "?" + encoded
	}
	_, body, err := c.download(path, inlineFilesLimit+1)
	if err != nil {
		return nil, err
	}
	if len(body) > inlineFilesLimit {
		return nil, &toolError{"These articles fill more than " + inlineFilesLimitWords + " of CSV, too large for one answer. Narrow the filters, or give the person you work for the path " + path + " on this Colliedesk to download them all."}
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xEF\xBB\xBF")))).ReadAll()
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("read the knowledge base export: %w", err)
	}
	rows := records[1:]
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	var text strings.Builder
	if err := csv.NewWriter(&text).WriteAll(append(records[:1:1], rows...)); err != nil {
		return nil, fmt.Errorf("write the knowledge base export: %w", err)
	}
	return map[string]any{"csv": text.String(), "rows": len(rows), "truncated": truncated, "download_path": path,
		"next": "download_path is a path on this Colliedesk: put the address the person opens Colliedesk at in front of it; it downloads the whole file while they are signed in."}, nil
}

func kbTagsList(c *call, _ map[string]any) (any, error) {
	var page struct {
		Items json.RawMessage `json:"items"`
	}
	if err := c.get("/api/v1/kb/tags", nil, &page); err != nil {
		return nil, err
	}
	return map[string]any{"tags": page.Items}, nil
}
