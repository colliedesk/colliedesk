package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"
)

// TestHelpCenterPagesAfterAnEditLatencyBudget holds an article page to the inbox budget on its
// first visit after a knowledge base write, when its related articles are ranked again: the shared
// fixture at the article cap, a short article and a French one. Before each visit the knowledge
// base's version grows, as every write makes it, so no visit reads a related list kept from the
// one before.
func TestHelpCenterPagesAfterAnEditLatencyBudget(t *testing.T) {
	_, pool, _, slug := sharedKBBudget(t)
	api, _, _, _ := testAPIOnPool(t, pool, nil)
	site := helpCenterSite(api)
	ctx := context.Background()
	workspaceID := queryString(t, pool, `SELECT id::text FROM workspaces WHERE slug=$1`, slug)
	base := "/help/" + slug
	for name, path := range map[string]string{"article": base + "/a/1000-article-1000", "fr article": base + "/fr/a/1000-article-fr-1000"} {
		durations := make([]time.Duration, 20)
		for index := range durations {
			if _, err := pool.Exec(ctx, `UPDATE help_centers SET content_version=content_version+1 WHERE workspace_id=$1`, workspaceID); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			response := visit(site, path, fmt.Sprintf("198.51.100.%d", index+1))
			durations[index] = time.Since(started)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status=%d body=%.300s", path, response.Code, response.Body.String())
			}
		}
		sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
		p95 := durations[len(durations)*95/100-1]
		t.Logf("help center %s after an edit p95=%s", name, p95)
		if p95 > inboxLatencyBudget {
			t.Errorf("help center %s after an edit p95 %s exceeds %s", name, p95, inboxLatencyBudget)
		}
	}
}
