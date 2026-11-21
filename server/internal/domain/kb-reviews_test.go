package domain_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func refundsText(files domain.KBAttachments) domain.KBContent {
	return domain.KBContent{Title: "Refunds", Slug: "refunds", BodyMD: "Within 14 days.", CollectionIDs: []string{"b", "a"}, Attachments: files}
}

// A text that lists no file keeps the hash it had before texts listed files, so an approval given then
// still counts; nil and an empty list are the same text.
func TestATextWithoutFilesKeepsItsHash(t *testing.T) {
	// sha256 of ["Refunds","refunds","Within 14 days.","","",["a","b"]]
	const want = "1d4d1d69b8ec7bc2ac7f8db2126d3f5bb9de715acb5d96c1fe03938e8d35327b"
	if got := refundsText(nil).Hash(); got != want {
		t.Fatalf("hash without files = %s, want %s", got, want)
	}
	if got := refundsText(domain.KBAttachments{}).Hash(); got != want {
		t.Fatalf("hash with an empty list = %s, want %s", got, want)
	}
}

// The files are part of the text in their order: another order is another text an approval does not cover.
func TestTheFileOrderIsPartOfTheHash(t *testing.T) {
	// sha256 of ["Refunds","refunds","Within 14 days.","","",["a","b"],["f2","f1"]]
	const want = "d526252ec9cb122e86b4eaebaf72a74d0d39a456c46657f0665c87b61c747281"
	listed := refundsText(domain.KBAttachments{{ID: "f2", Name: "parts.csv"}, {ID: "f1", Name: "Warranty form.pdf"}})
	if got := listed.Hash(); got != want {
		t.Fatalf("hash with files = %s, want %s", got, want)
	}
	reordered := refundsText(domain.KBAttachments{{ID: "f1"}, {ID: "f2"}})
	if reordered.Hash() == listed.Hash() {
		t.Fatal("two file orders share one hash")
	}
}
