package domain

import (
	"strings"
	"testing"
	"time"
)

func TestParseAttributionKeepsOnlyListedKeysWithPlainValues(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	long := strings.Repeat("a", 300)
	cases := []struct {
		name  string
		input map[string]string
		want  Attribution
	}{
		{name: "nothing", input: nil, want: Attribution{}},
		{
			name:  "every utm key",
			input: map[string]string{"utm_source": "tiktok", "utm_medium": "social", "utm_campaign": "launch_2026", "utm_content": "post-07", "utm_term": "help.desk"},
			want:  Attribution{UTMSource: "tiktok", UTMMedium: "social", UTMCampaign: "launch_2026", UTMContent: "post-07", UTMTerm: "help.desk"},
		},
		{name: "unlisted keys are dropped", input: map[string]string{"utm_source": "x", "gclid": "abc", "email": "ana@example.com", "UTM_SOURCE": "y"}, want: Attribution{UTMSource: "x"}},
		{name: "a space is dropped", input: map[string]string{"utm_campaign": "spring sale"}, want: Attribution{}},
		{name: "markup is dropped", input: map[string]string{"utm_content": "<script>"}, want: Attribution{}},
		{name: "an empty value is dropped", input: map[string]string{"utm_term": ""}, want: Attribution{}},
		{name: "300 characters are kept", input: map[string]string{"utm_term": long}, want: Attribution{UTMTerm: long}},
		{name: "301 characters are dropped", input: map[string]string{"utm_term": long + "a"}, want: Attribution{}},
		{name: "non-ASCII letters are dropped", input: map[string]string{"utm_source": "tiktök"}, want: Attribution{}},
		{
			name:  "a click id keeps its case and the time it was first seen",
			input: map[string]string{"fbclid": "IwAR3xY_z-9.Q", "click_id_seen_at": "2026-11-01T09:30:00Z"},
			want:  Attribution{FBCLID: "IwAR3xY_z-9.Q", ClickIDSeenAt: time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)},
		},
		{
			name:  "a click id without a time was first seen now",
			input: map[string]string{"ttclid": "E.C.P.abc"},
			want:  Attribution{TTCLID: "E.C.P.abc", ClickIDSeenAt: now},
		},
		{
			name:  "a time in the future is replaced by now",
			input: map[string]string{"fbclid": "abc", "click_id_seen_at": "2026-11-01T11:00:00Z"},
			want:  Attribution{FBCLID: "abc", ClickIDSeenAt: now},
		},
		{
			name:  "a time older than a week is replaced by now",
			input: map[string]string{"fbclid": "abc", "click_id_seen_at": "2026-10-20T10:00:00Z"},
			want:  Attribution{FBCLID: "abc", ClickIDSeenAt: now},
		},
		{
			name:  "an unreadable time is replaced by now",
			input: map[string]string{"fbclid": "abc", "click_id_seen_at": "yesterday"},
			want:  Attribution{FBCLID: "abc", ClickIDSeenAt: now},
		},
		{name: "a time without a click id is dropped", input: map[string]string{"utm_source": "x", "click_id_seen_at": "2026-11-01T09:30:00Z"}, want: Attribution{UTMSource: "x"}},
		{name: "an invalid click id leaves no time", input: map[string]string{"fbclid": "a b", "click_id_seen_at": "2026-11-01T09:30:00Z"}, want: Attribution{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseAttribution(tc.input, now)
			if got != tc.want {
				t.Fatalf("ParseAttribution(%v)=%+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestAttributionPropertiesNameOnlyTheValuesGiven(t *testing.T) {
	if got := (Attribution{}).Properties(); got != nil {
		t.Fatalf("empty attribution properties=%v, want nil", got)
	}
	got := Attribution{UTMSource: "tiktok", UTMContent: "post-07", FBCLID: "abc", ClickIDSeenAt: time.Now()}.Properties()
	if len(got) != 3 || got["utm_source"] != "tiktok" || got["utm_content"] != "post-07" || got["fbclid"] != "abc" {
		t.Fatalf("properties=%v, want utm_source, utm_content and fbclid only", got)
	}
}
