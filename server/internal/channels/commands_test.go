package channels_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
)

func TestMatchCommandFoldsAccentsAndForgivesOneTypo(t *testing.T) {
	commands := map[string]channels.Command{
		"bug":     {Phrases: []string{"Báo lỗi"}, Tags: []string{"bug"}, Priority: "high"},
		"feature": {Phrases: []string{"Tính năng"}},
		"cancel":  {Action: channels.ActionCancel, Phrases: []string{"Hủy"}},
		"pl":      {Phrases: []string{"Zgłoś błąd"}},
		"da":      {Phrases: []string{"Fejl på øl"}},
		"de":      {Phrases: []string{"Straße"}},
	}
	for _, c := range []struct {
		text, want, rest string
	}{
		{"Báo lỗi: app không mở được", "bug", "app không mở được"},
		{"bao loi app khong mo duoc", "bug", "app khong mo duoc"},
		{"BAO LOI", "bug", ""},
		{"bao lio cái này", "bug", "cái này"}, // swapped letters
		{"bao loii", "bug", ""},               // one letter added
		{"tinh nang moi", "feature", "moi"},
		{"huy", "cancel", ""},
		{"zglos blad w aplikacji", "pl", "w aplikacji"},
		{"fejl pa ol i baren", "da", "i baren"},
		{"strasse 5", "de", "5"},
	} {
		name, command, rest, ok := channels.MatchCommand(c.text, commands)
		if !ok || name != c.want || rest != c.rest {
			t.Errorf("%q: %q rest=%q ok=%v, want %q rest=%q", c.text, name, rest, ok, c.want, c.rest)
		}
		if ok && command.Action == "" {
			t.Errorf("%q: no action", c.text)
		}
	}
	for _, text := range []string{"hux", "bao", "báo cáo lỗi", "hello báo lỗi", "", "tinh"} {
		if name, _, _, ok := channels.MatchCommand(text, commands); ok {
			t.Errorf("%q matched %q, want no command (a short phrase forgives nothing, a command starts the text)", text, name)
		}
	}
}

// match words folds accents but forgives no wrong letter (as a chat's own grammar does), takes a missing
// or doubled space between the words of a phrase, and never a phrase glued to the next word.
func TestMatchCommandWordsForgivesNoTypo(t *testing.T) {
	commands := map[string]channels.Command{
		"bug":     {Match: channels.MatchWords, Phrases: []string{"Báo lỗi", "#baoloi", "/loi"}},
		"feature": {Match: channels.MatchWords, Phrases: []string{"Tính năng"}},
	}
	for _, c := range []struct {
		text, want, rest string
	}{
		{"Báo lỗi: app không mở được", "bug", "app không mở được"},
		{"bao loi app", "bug", "app"},
		{"báolỗi app", "bug", "app"},
		{"báo   lỗi", "bug", ""},
		{"#baoloi in hoá đơn", "bug", "in hoá đơn"},
		{"/loi in", "bug", "in"},
		{"Tính năng: xuất Excel", "feature", "xuất Excel"},
	} {
		name, _, rest, ok := channels.MatchCommand(c.text, commands)
		if !ok || name != c.want || rest != c.rest {
			t.Errorf("%q: %q rest=%q ok=%v, want %q rest=%q", c.text, name, rest, ok, c.want, c.rest)
		}
	}
	for _, text := range []string{"bao lio", "bao loii", "báo lỗix", "lỗi in hoá đơn", "loi in", "báo cáo lỗi"} {
		if name, _, _, ok := channels.MatchCommand(text, commands); ok {
			t.Errorf("%q matched %q, want no command", text, name)
		}
	}
}

// A phrase that starts with # or / keeps it: "/loi" names the token, never a message starting with the word.
func TestMatchCommandKeepsTheSigilOfAPhrase(t *testing.T) {
	commands := map[string]channels.Command{"bug": {Phrases: []string{"/loi"}}}
	if _, _, _, ok := channels.MatchCommand("lỗi đăng nhập", commands); ok {
		t.Error(`"lỗi đăng nhập" matched "/loi"`)
	}
	if name, _, rest, ok := channels.MatchCommand("/loi đăng nhập", commands); !ok || name != "bug" || rest != "đăng nhập" {
		t.Errorf(`"/loi đăng nhập": %q rest=%q ok=%v`, name, rest, ok)
	}
}

// match exact is the whole message, its accents kept, in any case and either Unicode form.
func TestMatchCommandExactIsTheWholeMessage(t *testing.T) {
	commands := map[string]channels.Command{"cancel": {Action: channels.ActionCancel, Match: channels.MatchExact, Phrases: []string{"Hủy"}}}
	for _, text := range []string{"Hủy", "hủy", "HỦY", "  Hủy  ", "Hu\u0309y"} {
		if name, command, rest, ok := channels.MatchCommand(text, commands); !ok || name != "cancel" || rest != "" || command.Action != channels.ActionCancel {
			t.Errorf("%q: %q rest=%q ok=%v, want cancel", text, name, rest, ok)
		}
	}
	for _, text := range []string{"huy", "Hủy đi", "Hủy!", "Hũy"} {
		if name, _, _, ok := channels.MatchCommand(text, commands); ok {
			t.Errorf("%q matched %q, want no command", text, name)
		}
	}
}

// rest is the customer's own text after the phrase, however the phrase is glued to it or what comes
// before it: a colon with no space, a dash in front.
func TestMatchCommandRestIsTheTextAfterThePhrase(t *testing.T) {
	commands := map[string]channels.Command{"bug": {Match: channels.MatchWords, Phrases: []string{"Báo lỗi"}},
		"fuzzy": {Phrases: []string{"Tính năng"}}}
	for _, c := range []struct{ text, want, rest string }{
		{"báo lỗi:mất hóa đơn", "bug", "mất hóa đơn"},
		{"- báo lỗi mất hóa đơn", "bug", "mất hóa đơn"},
		{"Báo lỗi – mất hóa đơn", "bug", "mất hóa đơn"},
		{"tính năng,xuất Excel", "fuzzy", "xuất Excel"},
	} {
		name, _, rest, ok := channels.MatchCommand(c.text, commands)
		if !ok || name != c.want || rest != c.rest {
			t.Errorf("%q: %q rest=%q ok=%v, want %q rest=%q", c.text, name, rest, ok, c.want, c.rest)
		}
	}
}

// A sigil the phrase does not have is no part of the message's command: "huy" is "/huy" and "#huy".
func TestMatchCommandTakesASigilBeforeAPhraseWithout(t *testing.T) {
	commands := map[string]channels.Command{"cancel": {Action: channels.ActionCancel, Phrases: []string{"huy"}}}
	for _, text := range []string{"/huy", "#huy", "huy"} {
		if name, _, _, ok := channels.MatchCommand(text, commands); !ok || name != "cancel" {
			t.Errorf("%q: %q ok=%v, want cancel", text, name, ok)
		}
	}
}
