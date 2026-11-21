package kblinks_test

import (
	"slices"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/kblinks"
)

func TestFindReadsEveryAddressOfTheHelpCenter(t *testing.T) {
	site := kblinks.Site{Slug: "acme", Hosts: []string{"Help.Acme.example"}}
	body := `Hi Ada, try https://desk.example.com/help/acme/a/12-reset-a-password.
Or in German: [hier](https://desk.example.com/help/acme/de/a/13-passwort).
Our own domain: <https://help.acme.example/a/14> and https://HELP.acme.example/pt-BR/a/15-x?ref=mail#steps
The team host: https://acme.colliedesk.example/help/a/16-billing, again https://desk.example.com/help/acme/a/12
The old portal: https://acme.freshdesk.com/support/solutions/articles/7201-reset-password and
https://help.acme.example/de/support/solutions/articles/7202
Not ours: https://desk.example.com/help/other/a/17-x, https://elsewhere.example/a/18-y,
https://desk.example.com/help/acme/c/3-guides, https://desk.example.com/help/acme/a/0-zero, help.acme.example/a/19`
	links := kblinks.Find(body, site)
	if want := []int64{12, 13, 14, 15, 16}; !slices.Equal(links.Numbers, want) {
		t.Fatalf("numbers = %v, want %v", links.Numbers, want)
	}
	if want := []string{"7201", "7202"}; !slices.Equal(links.Freshdesk, want) {
		t.Fatalf("freshdesk ids = %v, want %v", links.Freshdesk, want)
	}
}

func TestFindWithoutLinksFindsNothing(t *testing.T) {
	links := kblinks.Find("Thanks, all sorted. See /help/acme/a/12 in the app.", kblinks.Site{Slug: "acme"})
	if len(links.Numbers) != 0 || len(links.Freshdesk) != 0 {
		t.Fatalf("links = %+v", links)
	}
}
