package freshdesk_test

import (
	"strconv"
	"testing"
)

// Freshdesk's callout cards (<pre class="fd-callout fd-callout--TYPE">) come in as the callouts the
// help center shows: note and info as Note, tip and idea as Tip, warning as Warning. A card of a type
// Colliedesk has no callout for stays a plain quote, never a code block.
func TestSolutionsFreshdeskCalloutsBecomeMarkdownCallouts(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	cases := map[string]struct{ folder, markup, body string }{
		"101": {"11", `<p>Before.</p><pre class="fd-callout fd-callout--warning">This deletes your data.</pre>`,
			"Before.\n\n> [!WARNING]\n> This deletes your data."},
		"102": {"11", `<pre class="fd-callout fd-callout--note">Exports run at night.</pre>`, "> [!NOTE]\n> Exports run at night."},
		"103": {"12", `<pre class="fd-callout fd-callout--info">Line one.<br>Line <strong>two</strong>.</pre>`,
			// A line break stays one: CommonMark's hard break is two spaces at the end of the line.
			"> [!NOTE]\n> Line one.  \n> Line **two**."},
		"104": {"111", `<pre class="fd-callout fd-callout--tip">Press / to search.</pre>`, "> [!TIP]\n> Press / to search."},
		"105": {"1111", `<pre class="fd-callout fd-callout--idea">Try a saved reply.</pre>`, "> [!TIP]\n> Try a saved reply."},
		"106": {"21", `<pre class="fd-callout fd-callout--success">Done.</pre>`, "> Done."},
	}
	env.editSolutions(func(fixture map[string]any) {
		for id, c := range cases {
			number, _ := strconv.ParseFloat(id, 64)
			fixtureArticle(fixture, c.folder, number)["description"] = c.markup
		}
	})
	env.mustRun(t)
	articles := env.kbArticles(t, env.workspaceID)
	for id, c := range cases {
		if got := articles[id].Body; got != c.body {
			t.Errorf("article %s body = %q, want %q", id, got, c.body)
		}
	}
}
