package mcpserver

import "strings"

type docsArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type docResult struct {
	Page    string `json:"page"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Excerpt string `json:"excerpt"`
}

type docsOut struct {
	Results []docResult `json:"results"`
	Count   int         `json:"count"`
	Hint    string      `json:"hint,omitempty"`
}

func docsSearch(_ *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[docsArgs](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, &toolError{"The query is empty. Give the words to look for in query."}
	}
	if args.Limit == 0 {
		args.Limit = 5
	}
	out := docsOut{Results: []docResult{}}
	for _, hit := range searchDocs(docSections(), args.Query) {
		if len(out.Results) == args.Limit {
			break
		}
		out.Results = append(out.Results, docResult{Page: hit.section.page, Title: hit.section.title, Section: hit.section.heading, Excerpt: excerpt(hit.section, args.Query)})
	}
	out.Count = len(out.Results)
	if out.Count == 0 {
		out.Hint = "No section matched. Try different words, or fewer words."
	}
	return out, nil
}
