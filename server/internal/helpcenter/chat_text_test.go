package helpcenter

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"testing"
)

var chatPlaceholder = regexp.MustCompile(`\{[a-z]+\}`)

// The help chat speaks every language the help center's own words do: each set has every word, with
// the placeholders the English one has (chat.js fills them), and a locale finds its set as a page does.
func TestTheChatHasEveryWordInEveryLanguageTheHelpCenterHas(t *testing.T) {
	for locale := range uiTexts {
		if _, ok := chatTexts[locale]; !ok {
			t.Errorf("no chat words for %s", locale)
		}
	}
	for locale := range chatTexts {
		if _, ok := uiTexts[locale]; !ok {
			t.Errorf("chat words for %s, which the help center does not have", locale)
		}
	}
	english := reflect.ValueOf(chatTexts["en"])
	kind := english.Type()
	for locale, text := range chatTexts {
		value := reflect.ValueOf(text)
		for i := range value.NumField() {
			field, word := kind.Field(i), value.Field(i).String()
			if word == "" {
				t.Errorf("%s: %s is empty", locale, field.Name)
				continue
			}
			want, got := chatPlaceholder.FindAllString(english.Field(i).String(), -1), chatPlaceholder.FindAllString(word, -1)
			slices.Sort(want)
			slices.Sort(got)
			if !slices.Equal(want, got) {
				t.Errorf("%s: %s has %v, want %v: %q", locale, field.Name, got, want, word)
			}
		}
	}
	for locale, want := range map[string]string{"fr-CA": "fr", "PT-BR": "pt-br", "pt-PT": "pt", "zh-Hans": "zh-cn", "zh-TW": "en", "xx": "en", "": "en"} {
		if ChatWords(locale) != chatTexts[want] {
			t.Errorf("ChatWords(%q) is not the %s set", locale, want)
		}
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(ChatWordsJSON("fr")), &decoded); err != nil || decoded["newChat"] != chatTexts["fr"].NewChat || len(decoded) != kind.NumField() {
		t.Fatalf("ChatWordsJSON(fr) = %v (%v)", decoded, err)
	}
}
