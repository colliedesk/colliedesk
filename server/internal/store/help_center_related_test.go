package store_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// relatedKB writes the articles of one workspace for the related-articles tests.
type relatedKB struct {
	t         *testing.T
	ctx       context.Context
	s         *store.Store
	workspace string
}

func (k relatedKB) collection(name string, parent *string, visibility string) string {
	k.t.Helper()
	c, err := k.s.CreateKBCollection(k.ctx, k.workspace, store.Actor{}, store.KBCollectionInput{
		Name: name, Slug: domain.Slugify(name, "c"), ParentID: parent, Visibility: visibility,
	})
	if err != nil {
		k.t.Fatalf("collection %s: %v", name, err)
	}
	return c.ID
}

func (k relatedKB) article(collection, title, body, status string, extra ...string) domain.KBArticle {
	k.t.Helper()
	a, err := k.s.CreateKBArticle(k.ctx, k.workspace, store.Actor{}, store.KBArticleInput{
		CollectionID: collection, Title: title, BodyMD: body, Status: status, ExtraCollectionIDs: extra,
	})
	if err != nil {
		k.t.Fatalf("article %s: %v", title, err)
	}
	return a
}

func relatedTitles(t *testing.T, s *store.Store, workspace, locale string, number int64) []string {
	t.Helper()
	related, err := s.HelpCenterRelatedArticles(context.Background(), workspace, locale, number, 5)
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, 0, len(related))
	for _, a := range related {
		if a.Number == number {
			t.Errorf("the article itself is listed as related to itself: %+v", a)
		}
		titles = append(titles, a.Title)
	}
	return titles
}

const (
	refundTitle = "Refund duplicate charge"
	refundBody  = "Open the invoice, pick the duplicate charge and press refund. The refund reaches the card within five days."
)

// A reader on the wrong article sees the articles closest to it in words first, from any collection,
// then the rest of its own collection in that collection's order, five at most. An article no reader
// of the help center may open never shows, however close its words are: a draft, one in the trash,
// one under a team-only collection three levels up, one under a help-chat-only collection, one also
// listed in a team-only collection, and the articles of another workspace.
func TestRelatedArticlesRankByContentThenFillFromTheCollection(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	k := relatedKB{t, ctx, s, testWorkspace(t, pool, "kb-related")}
	billing := k.collection("Billing", nil, "")
	account := k.collection("Account", nil, "")
	internal := k.collection("Internal", nil, domain.KBVisibleAgents)
	team := k.collection("Team", &internal, "")
	notes := k.collection("Notes", &team, "")
	bot := k.collection("Bot", nil, domain.KBVisibleHelpChat)
	answers := k.collection("Answers", &bot, "")
	private := k.collection("Private", nil, domain.KBVisibleAgents)

	source := k.article(billing, refundTitle, refundBody, "published")
	// Every visible article has "the" and "and": words that most articles hold say nothing about
	// what an article is about, so "Team seats" shares no word that counts, though it comes first in
	// the workspace's order.
	k.article(account, "Team seats", "Add the seat and name the person.", "published")
	k.article(billing, "Apply coupon", "Type the code at checkout and save.", "published")
	k.article(billing, "Billing address", "Change the street and city on your profile.", "published")
	k.article(billing, "Currency", "Choose the currency and keep it.", "published")
	k.article(billing, "Download receipts", "Export the receipts and print them.", "published")
	k.article(billing, "Card expiry", "Update the card before it expires and billing goes on.", "published")
	k.article(account, "Duplicate charge refund", "When the card shows the duplicate charge, refund it from the invoice and wait.", "published")
	k.article(account, "Charge on card", "Why the charge shows on the card statement and what to do.", "published")

	// Each of these has the source's own words, closer than any visible article.
	draft := k.article(billing, refundTitle+" draft", refundBody, "draft")
	trashed := k.article(billing, refundTitle+" trashed", refundBody, "published")
	if err := s.DeleteKBArticle(ctx, k.workspace, store.Actor{}, trashed.ID, trashed.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	k.article(notes, refundTitle+" team three levels down", refundBody, "published")
	k.article(answers, refundTitle+" help chat", refundBody, "published")
	k.article(account, refundTitle+" listed in private", refundBody, "published", private)
	other := relatedKB{t, ctx, s, testWorkspace(t, pool, "kb-related-other")}
	otherBilling := other.collection("Billing", nil, "")
	other.article(otherBilling, refundTitle+" other workspace", refundBody, "published")

	// Each match shares more of the source's words than the next one, so the order holds whatever
	// weight a word carries; "Card expiry" matches too, so it is listed once, with the matches.
	want := []string{"Duplicate charge refund", "Charge on card", "Card expiry", "Apply coupon", "Billing address"}
	if got := relatedTitles(t, s, k.workspace, "", source.Number); !slices.Equal(got, want) {
		t.Fatalf("related = %q, want %q", got, want)
	}
	// An article no reader may open has nothing related.
	if got := relatedTitles(t, s, k.workspace, "", draft.Number); len(got) != 0 {
		t.Fatalf("related to the draft = %q, want none", got)
	}
}

// On a small knowledge base one shared word is a match, and an article that shares no word with
// anything outside its collection, alone in it, has nothing related.
func TestRelatedArticlesOnASmallKnowledgeBase(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	k := relatedKB{t, ctx, s, testWorkspace(t, pool, "kb-related-small")}
	billing := k.collection("Billing", nil, "")
	account := k.collection("Account", nil, "")
	lonely := k.collection("Lonely", nil, "")
	source := k.article(billing, "Refund duplicate", "Press refund.", "published")
	k.article(account, "Duplicate seats", "Remove one seat.", "published")
	alone := k.article(lonely, "Webhooks", "Signing keys rotate.", "published")
	if got, want := relatedTitles(t, s, k.workspace, "", source.Number), []string{"Duplicate seats"}; !slices.Equal(got, want) {
		t.Fatalf("related = %q, want %q", got, want)
	}
	if got := relatedTitles(t, s, k.workspace, "", alone.Number); len(got) != 0 {
		t.Fatalf("related to an article with nothing close = %q, want none", got)
	}
}

// A long article full of words of its own, links and images still finds the article about the same
// thing before the rest of its collection.
func TestRelatedArticlesOfALongArticle(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	k := relatedKB{t, ctx, s, testWorkspace(t, pool, "kb-related-long")}
	developers := k.collection("Developers", nil, "")
	security := k.collection("Security", nil, "")
	var body strings.Builder
	body.WriteString("See https://example.com/docs/webhooks/signing-secrets for the details.\n\n")
	body.WriteString("![Signing](/kb/media/0b6a3f1e-9c2d-4e8f-a1b2-c3d4e5f60718)\n\n")
	for i := range 150 {
		fmt.Fprintf(&body, "considerationsof%c%c ", 'a'+rune(i/26), 'a'+rune(i%26))
	}
	source := k.article(developers, "Rotate webhook secret", body.String(), "published")
	k.article(developers, "Api limits", "Requests per minute.", "published")
	k.article(security, "Webhook secret rotation", "Make a new secret before the old one stops.", "published")
	got := relatedTitles(t, s, k.workspace, "", source.Number)
	if want := []string{"Webhook secret rotation", "Api limits"}; !slices.Equal(got, want) {
		t.Fatalf("related = %q, want %q", got, want)
	}
}

// In another language the related articles are the ones published and shown in it, with their title
// and slug in it; an article not translated is left out, as on every other help center list, and a
// language the help center hides has none.
func TestRelatedArticlesInTheReadersLanguage(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	k := relatedKB{t, ctx, s, testWorkspace(t, pool, "kb-related-fr")}
	if _, err := s.SetHelpCenterLanguages(ctx, k.workspace, []domain.HelpCenterLanguageInput{{Locale: "fr", Visible: true}, {Locale: "de", Visible: false}}); err != nil {
		t.Fatal(err)
	}
	billing := k.collection("Billing", nil, "")
	account := k.collection("Account", nil, "")
	translate := func(a domain.KBArticle, locale, title, slug, body string) {
		t.Helper()
		if _, err := s.CreateKBTranslation(ctx, k.workspace, store.Actor{}, a.ID, store.KBTranslationInput{
			Locale: locale, Title: title, Slug: slug, BodyMD: body, Status: "published",
		}); err != nil {
			t.Fatal(err)
		}
	}
	source := k.article(billing, refundTitle, refundBody, "published")
	translate(source, "fr", "Rembourser un double paiement", "rembourser-double-paiement", "Ouvrez la facture et remboursez le double paiement.")
	translate(source, "de", "Doppelte Zahlung erstatten", "doppelte-zahlung", "Öffnen Sie die Rechnung und erstatten Sie die doppelte Zahlung.")
	match := k.article(account, "Duplicate charge refund", "Refund the duplicate charge from the invoice.", "published")
	translate(match, "fr", "Double paiement remboursé", "double-paiement-rembourse", "Remboursez le double paiement depuis la facture.")
	translate(match, "de", "Doppelte Zahlung erstattet", "doppelte-zahlung-erstattet", "Erstatten Sie die doppelte Zahlung.")
	k.article(account, "Duplicate charge on card", "The duplicate charge and refund of the card.", "published")

	related, err := s.HelpCenterRelatedArticles(ctx, k.workspace, "fr", source.Number, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(related) != 1 || related[0].Number != match.Number || related[0].Title != "Double paiement remboursé" || related[0].Slug != "double-paiement-rembourse" {
		t.Fatalf("related in French = %+v, want only the translated match with its French title and slug", related)
	}
	if got := relatedTitles(t, s, k.workspace, "de", source.Number); len(got) != 0 {
		t.Fatalf("related in a hidden language = %q, want none", got)
	}
}
