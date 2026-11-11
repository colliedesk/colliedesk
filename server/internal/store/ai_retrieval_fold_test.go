package store_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A search folds case and accents the same way in every language: a mark written apart from its letter
// is dropped too, and đ, ł and ø, which have no mark, are folded by hand.
func TestFoldingDropsCaseAndAccents(t *testing.T) {
	for text, want := range map[string]string{
		"Đăng NHẬP":                 "dang nhap",
		"Cafe\u0301 ÉTÉ, Ølé, Łódź": "cafe ete, ole, lodz",
		"Order #42 SHIPS (Tuesday)": "order #42 ships (tuesday)",
		"日本語 Straße":                "日本語 straße",
		"":                          "",
	} {
		if got := store.FoldForAIRetrieval(text); got != want {
			t.Errorf("FoldForAIRetrieval(%q) = %q, want %q", text, got, want)
		}
	}
}
