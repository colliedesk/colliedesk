package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/colliedesk/colliedesk/server/internal/aidraft"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// reviewOnly runs only the review of the high-confidence drafts of an earlier, graded run, and reports
// what precision and coverage would be with it: a cheap way to tune the review against the grades.
func reviewOnly(ctx context.Context, provider aiprovider.Provider, settings aidraft.Settings, data dataset, grades []grade, workers int) string {
	byTitle := map[string]article{}
	for _, a := range data.Articles {
		byTitle[a.Title] = a
	}
	type outcome struct {
		grade    grade
		blocking []string
		failed   bool
	}
	var high []grade
	answerable := 0
	for _, g := range grades {
		if g.Case.Kind == "answerable" {
			answerable++
		}
		if g.Draft != nil && g.Draft.Confidence != nil && *g.Draft.Confidence == "high" && g.Draft.Body != nil {
			high = append(high, g)
		}
	}
	results := make([]outcome, len(high))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				g := high[i]
				in := aidraft.Input{Subject: g.Case.Subject, Turns: []aidraft.Turn{{From: aidraft.FromCustomer, Text: g.Case.Message}}}
				result := aidraft.Result{Body: *g.Draft.Body, Confidence: aidraft.ConfidenceHigh}
				for _, source := range g.Draft.Sources {
					a, ok := byTitle[source.Title]
					if !ok || source.Kind != "kb_article" {
						continue
					}
					in.Sources = append(in.Sources, store.AIRetrievedDoc{Kind: store.AIRetrievedKBArticle, KBArticleID: source.ID, Title: a.Title, Text: cutText(a.Body, 6000)})
					result.Sources = append(result.Sources, aidraft.Source{Kind: source.Kind, ID: source.ID, Title: source.Title})
				}
				response, err := provider.Complete(ctx, aidraft.BuildReviewRequest(in, result, settings))
				review, ok := aidraft.ParseReview(response.JSON)
				results[i] = outcome{grade: g, blocking: review.Blocking, failed: err != nil || !ok}
			}
		}()
	}
	for i := range high {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	tally := map[string]map[string]int{"passed": {}, "flagged": {}}
	passedAnswerable, passedCorrect, passedAnswerableTotal := 0, 0, 0
	var b strings.Builder
	for _, r := range results {
		side := "passed"
		if r.failed || len(r.blocking) > 0 {
			side = "flagged"
		}
		tally[side][r.grade.Verdict]++
		if side == "passed" && r.grade.Case.Kind == "answerable" {
			passedAnswerableTotal++
			if r.grade.Verdict == verdictCorrect {
				passedCorrect++
			}
			passedAnswerable++
		}
		if side == "flagged" && r.grade.Verdict == verdictCorrect {
			fmt.Fprintf(&b, "  flagged but correct %s: %s\n", r.grade.Case.ID, cutText(strings.Join(r.blocking, "; "), 200))
		}
	}
	return fmt.Sprintf("review-only on %d high drafts: passed %v, flagged %v\nafter review: answerable high %d of %d (coverage %s), correct as written %d/%d = %s (95%% lower bound %.1f%%)\n%s",
		len(high), tally["passed"], tally["flagged"], passedAnswerable, answerable, percent(passedAnswerable, answerable),
		passedCorrect, passedAnswerableTotal, percent(passedCorrect, passedAnswerableTotal), 100*wilsonLow(passedCorrect, passedAnswerableTotal), b.String())
}
