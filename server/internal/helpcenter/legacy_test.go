package helpcenter

import "testing"

// Old Freshdesk help center paths, written by hand from the addresses a Freshdesk portal shows.
func TestParseLegacyPathReadsOldFreshdeskAddresses(t *testing.T) {
	cases := []struct {
		path string
		want LegacyAddress
		ok   bool
	}{
		{"/support", LegacyAddress{Kind: LegacyHome}, true},
		{"/support/", LegacyAddress{Kind: LegacyHome}, true},
		{"/support/home", LegacyAddress{Kind: LegacyHome}, true},
		{"/support/solutions", LegacyAddress{Kind: LegacyHome}, true},
		{"/support/solutions/", LegacyAddress{Kind: LegacyHome}, true},
		{"/en/support/home", LegacyAddress{Kind: LegacyHome}, true},
		{"/support/solutions/12000004321", LegacyAddress{Kind: LegacyCategory, ID: "12000004321"}, true},
		{"/support/solutions/12000004321-getting-started", LegacyAddress{Kind: LegacyCategory, ID: "12000004321", Words: "getting started"}, true},
		{"/support/solutions/folders/12000007788", LegacyAddress{Kind: LegacyFolder, ID: "12000007788"}, true},
		{"/pt-BR/support/solutions/folders/12000007788-faturas/", LegacyAddress{Kind: LegacyFolder, ID: "12000007788", Words: "faturas"}, true},
		{"/support/solutions/articles/7201", LegacyAddress{Kind: LegacyArticle, ID: "7201"}, true},
		{"/de/support/solutions/articles/7201-reset-password", LegacyAddress{Kind: LegacyArticle, ID: "7201", Words: "reset password"}, true},
		{"/support/solutions/articles/7201-how-do-i_pay--by-card?", LegacyAddress{Kind: LegacyArticle, ID: "7201", Words: "how do i pay by card"}, true},
		{"/support/search", LegacyAddress{Kind: LegacySearch}, true},
		{"/en/support/search/solutions", LegacyAddress{Kind: LegacySearch}, true},
		// Shapes that name no item still belong to the old portal: they go to the home page.
		{"/support/solutions/folders/x", LegacyAddress{Kind: LegacyOther}, true},
		{"/support/solutions/articles/", LegacyAddress{Kind: LegacyOther}, true},
		{"/support/solutions/articles/0123", LegacyAddress{Kind: LegacyOther}, true},
		{"/support/solutions/articles/7201/extra", LegacyAddress{Kind: LegacyOther}, true},
		{"/support/tickets/new", LegacyAddress{Kind: LegacyOther}, true},
		{"/support/login", LegacyAddress{Kind: LegacyOther}, true},
		// Not the old portal at all.
		{"/", LegacyAddress{}, false},
		{"/supporters", LegacyAddress{}, false},
		{"/a/7201-reset", LegacyAddress{}, false},
		{"/c/support", LegacyAddress{}, false},
		{"/english/support/home", LegacyAddress{}, false},
		{"//evil.example/support/home", LegacyAddress{}, false},
		{"/de/de/support/home", LegacyAddress{}, false},
		{"/de-/support/home", LegacyAddress{}, false},
	}
	for _, c := range cases {
		got, ok := ParseLegacyPath(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseLegacyPath(%q) = %+v, %v; want %+v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestLegacyAddressNamesTheLedgerEntity(t *testing.T) {
	for kind, want := range map[LegacyKind]string{LegacyCategory: "kb_category", LegacyFolder: "kb_folder", LegacyArticle: "kb_article",
		LegacyHome: "", LegacySearch: "", LegacyOther: ""} {
		if got := (LegacyAddress{Kind: kind, ID: "1"}).Entity(); got != want {
			t.Errorf("kind %d entity = %q, want %q", kind, got, want)
		}
	}
}

func TestLegacyWordsAreBounded(t *testing.T) {
	long := "/support/solutions/articles/1-"
	for range 150 {
		long += "word-"
	}
	got, ok := ParseLegacyPath(long)
	if !ok || got.Kind != LegacyArticle || len([]rune(got.Words)) > maxSearchChars {
		t.Fatalf("long slug: %+v %v (%d characters)", got.Kind, ok, len([]rune(got.Words)))
	}
}
