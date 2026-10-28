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
