package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// Verdicts of the grader.
const (
	verdictCorrect       = "correct"        // a support lead sends it as written
	verdictMinorEdit     = "minor_edit"     // right and safe, needs a small edit
	verdictWrong         = "wrong"          // a wrong or unsupported fact, a promise, misses the question, or answers what needed a person
	verdictHandoffOK     = "handoff_ok"     // handing over or asking was the right call
	verdictHandoffMissed = "handoff_missed" // handed over or asked although the knowledge base answered it
)

type grade struct {
	draftRow
	Verdict     string   `json:"verdict"`
	Errors      []string `json:"errors"`
	Explanation string   `json:"explanation"`
	TriageOK    *bool    `json:"triage_ok"`
	TriageNote  string   `json:"triage_note"`
}

const graderSystem = `You are the support lead of Hover (a domain and email provider) and you grade reply drafts an AI wrote for your team, strictly. A draft graded "correct" may be sent to the customer with no person reading it, so be exacting.

You get the customer's email, what the team knows about the case, the help center article(s) that answer it (the truth), the articles the draft cited, and the draft.

Verdicts:
- "correct": you would send it exactly as written (a greeting or a word changed at most). Every fact, step, button name, price, time limit and link agrees with the truth or with a cited article, it answers everything the customer asked, it is in the customer's language, and it promises nothing the team has not decided (no refund, credit, exception, deadline or fix date).
- "minor_edit": right and safe, but you would change something small before sending (a detail missing that the customer will likely need, an unclear step, too long).
- "wrong": any fact that is wrong or not supported by the truth or the cited articles, a step that would not work, a promise the team did not make, missing the customer's main question, the wrong language, or answering on its own a case that needed a person (the customer's own account, a refund, an angry escalation, an outage, legal, security).
- "handoff_ok": the draft handed the case to a person, or asked the customer one question, and that was the right call: the knowledge base could not settle it, or only a person can act, or the question was really needed to choose the right answer.
- "handoff_missed": the draft handed over or asked, but the article in the knowledge base answered the email well enough to reply.

"errors": each wrong or unsupported statement, promise or omission, one per item (empty when none).
"explanation": two sentences at most.

You also grade the triage suggestion: the tags chosen from the team's tags (domains, dns, email, transfers, billing, account-security, website-builders, webmail, renewals) and the priority (urgent: cannot work at all, data loss, security, many people affected; high: blocked on something important or upset; normal: most tickets, including how-to questions and anything not working as expected; low: only feedback, an idea or a thank-you). "triage_ok" is true when a support lead would keep it: the tags fit (a missing secondary tag is fine, a wrong tag is not) and the priority is the right one or one step off in the safe direction (higher). "triage_note": what is wrong (always say it when triage_ok is false), or empty.`

func graderSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"verdict", "errors", "explanation", "triage_ok", "triage_note"},
		"properties": map[string]any{
			"triage_ok":   map[string]any{"type": "boolean"},
			"triage_note": map[string]any{"type": "string"},
			"verdict":     map[string]any{"type": "string", "enum": []string{verdictCorrect, verdictMinorEdit, verdictWrong, verdictHandoffOK, verdictHandoffMissed}},
			"errors":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"explanation": map[string]any{"type": "string"},
		},
	}
}

func expectation(c evalCase) string {
	switch c.Kind {
	case "answerable":
		return "The knowledge base the drafter could read contains the article below, which answers this email."
	case "held_out":
		return "The article below answers this email, but it was NOT in the knowledge base the drafter could read. Unless another cited article really answers it, the right call is to hand over or ask."
	default:
		return "This email needs a person on the team (" + strings.SplitN(c.Detail, ":", 2)[0] + "): the knowledge base alone cannot settle it. The article below is only related context. A draft may give general guidance from the articles, but must not claim to resolve the case, decide for the team, or promise anything."
	}
}

// gradeAll grades the drafts that have no grade in done yet, until the deadline, and returns every grade.
func gradeAll(ctx context.Context, provider aiprovider.Provider, model string, data dataset, drafts []draftRow, done []grade, workers int, deadline time.Time) []grade {
	byID, byTitle := map[string]article{}, map[string]article{}
	for _, a := range data.Articles {
		byID[a.ID], byTitle[a.Title] = a, a
	}
	graded := map[string]bool{}
	for _, g := range done {
		if g.Verdict != "ungraded" {
			graded[g.Case.ID] = true
		}
	}
	grades := make([]grade, 0, len(drafts))
	for _, g := range done {
		if graded[g.Case.ID] {
			grades = append(grades, g)
		}
	}
	var mu sync.Mutex
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				g := gradeOne(ctx, provider, model, byID, byTitle, drafts[i])
				mu.Lock()
				grades = append(grades, g)
				mu.Unlock()
			}
		}()
	}
	for i := range drafts {
		if graded[drafts[i].Case.ID] {
			continue
		}
		if time.Now().After(deadline) {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	sort.Slice(grades, func(i, j int) bool { return grades[i].Case.ID < grades[j].Case.ID })
	return grades
}

func gradeOne(ctx context.Context, provider aiprovider.Provider, model string, byID, byTitle map[string]article, row draftRow) grade {
	out := grade{draftRow: row}
	if row.Draft == nil || row.Draft.Status != "ready" || row.Draft.Action == nil {
		out.Verdict, out.Explanation = "no_draft", "the worker wrote no draft"
		if row.Draft != nil && row.Draft.ErrorCode != nil {
			out.Explanation += ": " + *row.Draft.ErrorCode
		}
		return out
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<customer_email subject=%q>\n%s\n</customer_email>\n\n<team_knows>%s</team_knows>\n\n", row.Case.Subject, row.Case.Message, expectation(row.Case))
	for _, id := range row.Case.ArticleIDs {
		a := byID[id]
		fmt.Fprintf(&b, "<truth_article title=%q>\n%s\n</truth_article>\n\n", a.Title, cutText(a.Body, 14000))
	}
	for _, source := range row.Draft.Sources {
		if a, ok := byTitle[source.Title]; ok && source.Kind == "kb_article" {
			fmt.Fprintf(&b, "<cited_article title=%q>\n%s\n</cited_article>\n\n", a.Title, cutText(a.Body, 8000))
		}
	}
	body := ""
	if row.Draft.Body != nil {
		body = *row.Draft.Body
	}
	handoff := ""
	if row.Draft.HandoffReason != nil {
		handoff = *row.Draft.HandoffReason
	}
	triage := "none"
	if row.Draft.Triage != nil {
		triage = fmt.Sprintf("tags %v, priority %q", row.Draft.Triage.Tags, row.Draft.Triage.Priority)
	}
	fmt.Fprintf(&b, "<draft action=%q>\n%s\n</draft>\n<note_for_team>%s</note_for_team>\n<triage>%s</triage>\n\nGrade the draft and the triage.", *row.Draft.Action, body, handoff, triage)
	request := aiprovider.Request{Model: model, Effort: aiprovider.EffortMedium, System: graderSystem, MaxOutput: 1500, Schema: graderSchema(),
		Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: b.String()}}}
	for attempt := range 3 {
		response, err := provider.Complete(ctx, request)
		if err != nil {
			log.Printf("grade %s attempt %d: %v", row.Case.ID, attempt+1, err)
			continue
		}
		var verdict struct {
			Verdict     string   `json:"verdict"`
			Errors      []string `json:"errors"`
			Explanation string   `json:"explanation"`
			TriageOK    bool     `json:"triage_ok"`
			TriageNote  string   `json:"triage_note"`
		}
		if err := json.Unmarshal([]byte(response.JSON), &verdict); err != nil {
			continue
		}
		out.Verdict, out.Errors, out.Explanation = verdict.Verdict, verdict.Errors, verdict.Explanation
		out.TriageOK, out.TriageNote = &verdict.TriageOK, verdict.TriageNote
		return out
	}
	out.Verdict = "ungraded"
	return out
}

func cutText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + " […]"
}

// wilsonLow is the lower end of the 95 % Wilson interval of successes out of n.
func wilsonLow(successes, n int) float64 {
	if n == 0 {
		return 0
	}
	z, p, total := 1.96, float64(successes)/float64(n), float64(n)
	centre := p + z*z/(2*total)
	margin := z * math.Sqrt(p*(1-p)/total+z*z/(4*total*total))
	return (centre - margin) / (1 + z*z/total)
}

func percent(a, b int) string {
	if b == 0 {
		return "–"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(a)/float64(b))
}

func summarize(grades []grade, model, judge string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Colliedesk AI drafts eval: %d cases, drafting %s, grading %s\n\n", len(grades), model, judge)
	type cell struct{ total, correct, minor, wrong, handoffOK, handoffMissed, other int }
	tally := map[string]map[string]*cell{}
	add := func(kind, confidence, verdict string) {
		if tally[kind] == nil {
			tally[kind] = map[string]*cell{}
		}
		c := tally[kind][confidence]
		if c == nil {
			c = &cell{}
			tally[kind][confidence] = c
		}
		c.total++
		switch verdict {
		case verdictCorrect:
			c.correct++
		case verdictMinorEdit:
			c.minor++
		case verdictWrong:
			c.wrong++
		case verdictHandoffOK:
			c.handoffOK++
		case verdictHandoffMissed:
			c.handoffMissed++
		default:
			c.other++
		}
	}
	var seconds []float64
	var input, output int64
	high, highCorrect, highLenient := 0, 0, 0
	answerable, answerableHigh, answerableHighCorrect, answerableHighLenient := 0, 0, 0, 0
	falseHigh := 0
	for _, g := range grades {
		confidence := "none"
		if g.Draft != nil && g.Draft.Confidence != nil {
			confidence = *g.Draft.Confidence
		}
		add(g.Case.Kind, confidence, g.Verdict)
		input, output = input+g.Usage.Input, output+g.Usage.Output
		if g.Seconds > 0 {
			seconds = append(seconds, g.Seconds)
		}
		if g.Case.Kind == "answerable" {
			answerable++
		}
		if confidence != "high" {
			continue
		}
		high++
		if g.Verdict == verdictCorrect {
			highCorrect++
		}
		if g.Verdict == verdictCorrect || g.Verdict == verdictMinorEdit {
			highLenient++
		}
		if g.Case.Kind == "answerable" {
			answerableHigh++
			if g.Verdict == verdictCorrect {
				answerableHighCorrect++
			}
			if g.Verdict == verdictCorrect || g.Verdict == verdictMinorEdit {
				answerableHighLenient++
			}
		} else if g.Verdict == verdictWrong {
			falseHigh++
		}
	}
	fmt.Fprintf(&b, "GOAL  KB-answerable tickets rated high: %d of %d (coverage %s)\n", answerableHigh, answerable, percent(answerableHigh, answerable))
	fmt.Fprintf(&b, "      correct as written: %d/%d = %s (95%% lower bound %.1f%%)\n", answerableHighCorrect, answerableHigh, percent(answerableHighCorrect, answerableHigh), 100*wilsonLow(answerableHighCorrect, answerableHigh))
	fmt.Fprintf(&b, "      correct or minor edit: %d/%d = %s\n", answerableHighLenient, answerableHigh, percent(answerableHighLenient, answerableHigh))
	fmt.Fprintf(&b, "ALL   rated high: %d, correct as written %s, correct or minor edit %s; high on a ticket the KB cannot settle and wrong: %d\n\n",
		high, percent(highCorrect, high), percent(highLenient, high), falseHigh)
	fmt.Fprintf(&b, "%-15s %-7s %5s %7s %5s %5s %8s %8s %5s\n", "kind", "conf", "n", "correct", "minor", "wrong", "hand-ok", "hand-miss", "other")
	for _, kind := range []string{"answerable", "held_out", "not_answerable"} {
		for _, confidence := range []string{"high", "medium", "low", "none"} {
			if c := tally[kind][confidence]; c != nil {
				fmt.Fprintf(&b, "%-15s %-7s %5d %7d %5d %5d %8d %8d %5d\n", kind, confidence, c.total, c.correct, c.minor, c.wrong, c.handoffOK, c.handoffMissed, c.other)
			}
		}
	}
	triaged, triageOK := 0, 0
	for _, g := range grades {
		if g.TriageOK != nil {
			triaged++
			if *g.TriageOK {
				triageOK++
			}
		}
	}
	fmt.Fprintf(&b, "\nTRIAGE kept by the grader: %d/%d = %s (95%% lower bound %.1f%%)\n", triageOK, triaged, percent(triageOK, triaged), 100*wilsonLow(triageOK, triaged))
	sort.Float64s(seconds)
	if len(seconds) > 0 {
		fmt.Fprintf(&b, "\nlatency p50 %.1f s, p95 %.1f s; tokens in %d, out %d (%.0f in / %.0f out per case)\n", seconds[len(seconds)/2], seconds[len(seconds)*95/100],
			input, output, float64(input)/float64(len(grades)), float64(output)/float64(len(grades)))
	}
	b.WriteString("\nRated high but not correct as written:\n")
	for _, g := range grades {
		if g.Draft != nil && g.Draft.Confidence != nil && *g.Draft.Confidence == "high" && g.Verdict != verdictCorrect {
			fmt.Fprintf(&b, "- %s [%s] %s: %s %v\n", g.Case.ID, g.Case.Kind, g.Verdict, g.Explanation, g.Errors)
		}
	}
	b.WriteString("\nTriage the grader would change:\n")
	for _, g := range grades {
		if g.TriageOK != nil && !*g.TriageOK {
			fmt.Fprintf(&b, "- %s: %s\n", g.Case.ID, g.TriageNote)
		}
	}
	b.WriteString("\nKB-answerable but not rated high (why):\n")
	for _, g := range grades {
		if g.Case.Kind == "answerable" && (g.Draft == nil || g.Draft.Confidence == nil || *g.Draft.Confidence != "high") {
			reasons := []string{}
			if g.Draft != nil {
				reasons = g.Draft.ConfidenceReasons
			}
			fmt.Fprintf(&b, "- %s %s %v\n", g.Case.ID, g.Verdict, reasons)
		}
	}
	return b.String()
}
