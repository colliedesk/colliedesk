package helpcenter

import (
	"fmt"
	"strings"
)

// searchContactText is the words of the block under a search that found nothing, in one language.
// Contact holds the team's name and Subject the words searched for (%s each). A language without a set
// uses its language's or English (textKey).
type searchContactText struct {
	Title, Contact, Subject string
}

var searchContactTexts = map[string]searchContactText{
	"en":    {Title: "Can’t find it?", Contact: "Contact %s", Subject: "Can’t find: %s"},
	"fr":    {Title: "Vous ne trouvez pas ?", Contact: "Contacter %s", Subject: "Introuvable : %s"},
	"de":    {Title: "Nicht gefunden?", Contact: "%s kontaktieren", Subject: "Nicht gefunden: %s"},
	"es":    {Title: "¿No lo encuentras?", Contact: "Contactar con %s", Subject: "No encuentro: %s"},
	"it":    {Title: "Non lo trovi?", Contact: "Contatta %s", Subject: "Non trovo: %s"},
	"pt":    {Title: "Não encontra?", Contact: "Contactar %s", Subject: "Não encontro: %s"},
	"pt-br": {Title: "Não encontrou?", Contact: "Falar com %s", Subject: "Não encontrei: %s"},
	"nl":    {Title: "Niet gevonden?", Contact: "Neem contact op met %s", Subject: "Niet gevonden: %s"},
	"ja":    {Title: "見つかりませんか？", Contact: "%s に問い合わせる", Subject: "見つかりません: %s"},
	"zh-cn": {Title: "没有找到？", Contact: "联系 %s", Subject: "没有找到：%s"},
	"ko":    {Title: "찾을 수 없나요?", Contact: "%s에 문의하기", Subject: "찾을 수 없음: %s"},
	"vi":    {Title: "Không tìm thấy?", Contact: "Liên hệ %s", Subject: "Không tìm thấy: %s"},
	"pl":    {Title: "Nie możesz znaleźć?", Contact: "Skontaktuj się z %s", Subject: "Nie mogę znaleźć: %s"},
	"sv":    {Title: "Hittar du inte?", Contact: "Kontakta %s", Subject: "Hittar inte: %s"},
	"ru":    {Title: "Не нашли?", Contact: "Связаться с %s", Subject: "Не могу найти: %s"},
}

func searchContactTextFor(locale string) searchContactText {
	if text, ok := searchContactTexts[textKey(locale)]; ok {
		return text
	}
	return searchContactTexts["en"]
}

// searchContact is the block under a search that found nothing: the team's contact link, as a plain link.
type searchContact struct {
	Title, URL, Label string
}

// searchContactOf is the block for a search of query that found nothing on a page whose brand is data,
// of the team named team; nil when the workspace set up no contact link. A mail to the team's address
// starts with the words searched for, unless the address carries its own subject or body.
func searchContactOf(data page, team, query, locale string) *searchContact {
	contact := data.Contact
	if contact == nil {
		return nil
	}
	text := searchContactTextFor(locale)
	block := &searchContact{Title: text.Title, URL: contact.URL, Label: fmt.Sprintf(text.Contact, team)}
	if strings.HasPrefix(strings.ToLower(contact.URL), mailto) && !strings.Contains(contact.URL, "?") {
		block.URL = contact.URL + "?subject=" + mailtoEscape(fmt.Sprintf(text.Subject, query))
	}
	return block
}
