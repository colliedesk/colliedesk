package helpcenter

import (
	"fmt"
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

// stillHelpText is the words of the "Still need help?" block at the end of an article in one language.
// Email, Call, Subject and About hold one %s: the address, the number, the article's title and its
// address. A language without a set uses its language's or English (textKey).
type stillHelpText struct {
	Title, Contact, Email, Call, Subject, About string
}

var stillHelpTexts = map[string]stillHelpText{
	"en":    {Title: "Still need help?", Contact: "Contact us", Email: "Email %s", Call: "Call %s", Subject: "Question about: %s", About: "About this article: %s"},
	"fr":    {Title: "Besoin d’aide ?", Contact: "Nous contacter", Email: "Écrire à %s", Call: "Appeler le %s", Subject: "Question sur : %s", About: "À propos de cet article : %s"},
	"de":    {Title: "Brauchen Sie weitere Hilfe?", Contact: "Kontakt", Email: "E-Mail an %s", Call: "%s anrufen", Subject: "Frage zu: %s", About: "Zu diesem Artikel: %s"},
	"es":    {Title: "¿Necesitas más ayuda?", Contact: "Contáctanos", Email: "Escribir a %s", Call: "Llamar al %s", Subject: "Pregunta sobre: %s", About: "Sobre este artículo: %s"},
	"it":    {Title: "Serve altro aiuto?", Contact: "Contattaci", Email: "Scrivi a %s", Call: "Chiama il %s", Subject: "Domanda su: %s", About: "Su questo articolo: %s"},
	"pt":    {Title: "Ainda precisa de ajuda?", Contact: "Contacte-nos", Email: "Enviar email para %s", Call: "Ligar para %s", Subject: "Pergunta sobre: %s", About: "Sobre este artigo: %s"},
	"pt-br": {Title: "Ainda precisa de ajuda?", Contact: "Fale conosco", Email: "Enviar e-mail para %s", Call: "Ligar para %s", Subject: "Pergunta sobre: %s", About: "Sobre este artigo: %s"},
	"nl":    {Title: "Nog hulp nodig?", Contact: "Neem contact op", Email: "Mail naar %s", Call: "Bel %s", Subject: "Vraag over: %s", About: "Over dit artikel: %s"},
	"ja":    {Title: "まだお困りですか？", Contact: "お問い合わせ", Email: "%s にメール", Call: "%s に電話", Subject: "質問: %s", About: "この記事について: %s"},
	"zh-cn": {Title: "仍需要帮助？", Contact: "联系我们", Email: "发邮件至 %s", Call: "致电 %s", Subject: "问题：%s", About: "关于这篇文章：%s"},
	"ko":    {Title: "도움이 더 필요하신가요?", Contact: "문의하기", Email: "%s(으)로 이메일", Call: "%s(으)로 전화", Subject: "문의: %s", About: "이 문서 관련: %s"},
	"vi":    {Title: "Vẫn cần trợ giúp?", Contact: "Liên hệ", Email: "Gửi email tới %s", Call: "Gọi %s", Subject: "Câu hỏi về: %s", About: "Về bài viết này: %s"},
	"pl":    {Title: "Nadal potrzebujesz pomocy?", Contact: "Skontaktuj się z nami", Email: "Napisz na %s", Call: "Zadzwoń pod %s", Subject: "Pytanie o: %s", About: "Dotyczy artykułu: %s"},
	"sv":    {Title: "Behöver du mer hjälp?", Contact: "Kontakta oss", Email: "Mejla %s", Call: "Ring %s", Subject: "Fråga om: %s", About: "Om den här artikeln: %s"},
	"ru":    {Title: "Нужна помощь?", Contact: "Связаться с нами", Email: "Написать на %s", Call: "Позвонить %s", Subject: "Вопрос о статье: %s", About: "Об этой статье: %s"},
}

func stillHelpTextFor(locale string) stillHelpText {
	if text, ok := stillHelpTexts[textKey(locale)]; ok {
		return text
	}
	return stillHelpTexts["en"]
}

// stillHelp is the "Still need help?" block: the ways to reach the team the workspace set up, each
// about the article. All are plain links, so the block works without script.
type stillHelp struct {
	Title            string
	AskURL, AskLabel string
	ContactURL       string
	ContactLabel     string
	PhoneURL         template.URL
	PhoneLabel       string
}

// stillHelpOf is the block for an article whose page is data and whose address is address; nil when
// the workspace set up no way to reach the team. The chat opens about the article, and a mail to the
// team's address starts with its title and address.
func stillHelpOf(data page, number int64, title, address, locale string) *stillHelp {
	if data.AskLink == "" && data.Contact == nil && data.Phone == nil {
		return nil
	}
	text := stillHelpTextFor(locale)
	block := &stillHelp{Title: text.Title}
	if data.AskLink != "" {
		separator := "?"
		if strings.Contains(data.AskLink, "?") {
			separator = "&"
		}
		block.AskURL, block.AskLabel = data.AskLink+separator+"article="+strconv.FormatInt(number, 10), data.AskLabel
	}
	if contact := data.Contact; contact != nil {
		block.ContactURL, block.ContactLabel = contact.URL, text.Contact
		if strings.HasPrefix(strings.ToLower(contact.URL), mailto) {
			block.ContactLabel = fmt.Sprintf(text.Email, contact.Name)
			// An address that already carries its own subject or body is left as the team wrote it.
			if !strings.Contains(contact.URL, "?") {
				block.ContactURL = contact.URL + "?subject=" + mailtoEscape(fmt.Sprintf(text.Subject, title)) +
					"&body=" + mailtoEscape(fmt.Sprintf(text.About, address)+"\n\n")
			}
		}
	}
	if phone := data.Phone; phone != nil {
		block.PhoneURL, block.PhoneLabel = phone.URL, fmt.Sprintf(text.Call, phone.Number)
	}
	return block
}

// mailtoEscape escapes a mailto: header value: spaces are %20, never + (RFC 6068).
func mailtoEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}
