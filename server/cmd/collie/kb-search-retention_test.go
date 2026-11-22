package main

import (
	"testing"
	"time"
)

func TestHelpCenterSearchesAreKept180DaysUnlessTheServerSaysOtherwise(t *testing.T) {
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == "kb_searches.retention_days" {
				return value
			}
			return ""
		}
	}
	day := 24 * time.Hour
	for value, want := range map[string]time.Duration{"": 180 * day, "30": 30 * day, " 365 ": 365 * day, "3650": 3650 * day} {
		if got, err := kbSearchRetentionFromEnv(env(value)); err != nil || got != want {
			t.Errorf("%q: got %s, %v; want %s", value, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-5", "3651", "6 months", "1.5"} {
		if _, err := kbSearchRetentionFromEnv(env(bad)); err == nil || err.Error() != "kb_searches.retention_days must be a whole number of days from 1 to 3650" {
			t.Errorf("%q: got %v, want the days range error", bad, err)
		}
	}
	built, err := buildConfig(env("90"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := hotSettings(built).KBSearchRetention; got != 90*day {
		t.Fatalf("hot retention = %s, want 90 days", got)
	}
}
