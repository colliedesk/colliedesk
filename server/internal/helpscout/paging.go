package helpscout

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// maxPages stops a list that never ends (a page count that keeps growing).
const maxPages = 100000

// listAll walks every page of a HAL list endpoint: items are in _embedded.<key>, page numbers start
// at 1 and the last page is page.totalPages. It returns page.totalElements of the last page read.
func listAll[T any](ctx context.Context, c *client, path string, query url.Values, key string, each func(T) error) (int, error) {
	total := 0
	for page := 1; ; page++ {
		q := url.Values{}
		for name, values := range query {
			q[name] = values
		}
		q.Set("page", strconv.Itoa(page))
		var body struct {
			Embedded map[string]json.RawMessage `json:"_embedded"`
			Page     hsPage                     `json:"page"`
		}
		if err := c.getJSON(ctx, path, q, &body); err != nil {
			return 0, err
		}
		var items []T
		if raw, ok := body.Embedded[key]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return 0, fmt.Errorf("decode %s page %d: %w", path, page, err)
			}
		}
		for _, item := range items {
			if err := each(item); err != nil {
				return 0, err
			}
		}
		total = body.Page.TotalElements
		if page >= body.Page.TotalPages || len(items) == 0 {
			return total, nil
		}
		if page >= maxPages {
			return 0, fmt.Errorf("%s has more than %d pages; stopping", path, maxPages)
		}
	}
}

func trimSpace(value string) string { return strings.TrimSpace(value) }
