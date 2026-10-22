package domain

import (
	"math"
	"testing"
)

func TestReplySimilarityCountsSharedWordsInOrder(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"Select the invoices, then Export > ZIP.", "select the invoices then export zip", 1},
		// 6 words each, 5 shared in order ("then" changed to "and"): 5/6.
		{"Select the invoices then Export ZIP", "Select the invoices and Export ZIP", 5.0 / 6},
		// The sent reply adds four words to a four-word draft: 4 shared over the longer 8.
		{"Refunds take ten days", "Refunds take ten days to reach your card", 4.0 / 8},
		{"", "", 1},
		{"Hello", "", 0},
		// Same words, reversed order: only one word can be in order.
		{"one two three", "three two one", 1.0 / 3},
	}
	for _, c := range cases {
		if got := ReplySimilarity(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ReplySimilarity(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestAutoAnswerNeedsTheSwitchEnoughReviewsAndTheAccuracy(t *testing.T) {
	on := DefaultAISettings(true)
	on.AutoAnswerEnabled = true // 10 reviewed, 90 %
	cases := []struct {
		settings           AISettings
		reviewed, accepted int
		want               bool
	}{
		{on, 10, 9, true},
		{on, 10, 8, false},
		{on, 9, 9, false},
		{on, 20, 18, true},
		{on, 0, 0, false},
		{DefaultAISettings(true), 50, 50, false},
	}
	for _, c := range cases {
		if got := AutoAnswerEligible(c.settings, c.reviewed, c.accepted); got != c.want {
			t.Errorf("enabled=%v reviewed=%d accepted=%d: eligible=%v, want %v", c.settings.AutoAnswerEnabled, c.reviewed, c.accepted, got, c.want)
		}
	}
}
