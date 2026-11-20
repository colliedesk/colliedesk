package helpcenter

// voteText is the "Was this article helpful?" box's words in one language; Count is a format of the
// helpful votes and of all votes. A language without a set uses its language's or English (textKey).
type voteText struct {
	Question, Yes, No, Count, Thanks, Missing, MissingHint, Send, Sent string
}

var voteTexts = map[string]voteText{
	"en": {Question: "Was this article helpful?", Yes: "Yes", No: "No", Count: "%d out of %d found this helpful", Thanks: "Thanks for letting us know.",
		Missing: "What was missing?", MissingHint: "Optional. Please leave out email addresses and numbers.", Send: "Send", Sent: "Thanks, we will use it to improve this article."},
	"fr": {Question: "Cet article vous a-t-il aidé ?", Yes: "Oui", No: "Non", Count: "%d sur %d l’ont trouvé utile", Thanks: "Merci de nous l’avoir dit.",
		Missing: "Que manquait-il ?", MissingHint: "Facultatif. N’indiquez ni adresse e-mail ni numéro.", Send: "Envoyer", Sent: "Merci, nous nous en servirons pour améliorer cet article."},
	"de": {Question: "War dieser Artikel hilfreich?", Yes: "Ja", No: "Nein", Count: "%d von %d fanden das hilfreich", Thanks: "Danke für Ihre Rückmeldung.",
		Missing: "Was hat gefehlt?", MissingHint: "Optional. Bitte keine E-Mail-Adressen oder Nummern angeben.", Send: "Senden", Sent: "Danke, damit verbessern wir diesen Artikel."},
	"es": {Question: "¿Te ha resultado útil este artículo?", Yes: "Sí", No: "No", Count: "A %d de %d les resultó útil", Thanks: "Gracias por decírnoslo.",
		Missing: "¿Qué faltaba?", MissingHint: "Opcional. No incluyas correos electrónicos ni números.", Send: "Enviar", Sent: "Gracias, lo usaremos para mejorar este artículo."},
	"it": {Question: "Questo articolo ti è stato utile?", Yes: "Sì", No: "No", Count: "%d su %d lo hanno trovato utile", Thanks: "Grazie per avercelo detto.",
		Missing: "Che cosa mancava?", MissingHint: "Facoltativo. Non inserire indirizzi email né numeri.", Send: "Invia", Sent: "Grazie, lo useremo per migliorare questo articolo."},
	"pt": {Question: "Este artigo foi útil?", Yes: "Sim", No: "Não", Count: "%d de %d acharam útil", Thanks: "Obrigado por nos dizer.",
		Missing: "O que faltou?", MissingHint: "Opcional. Não inclua endereços de email nem números.", Send: "Enviar", Sent: "Obrigado, vamos usá-lo para melhorar este artigo."},
	"pt-br": {Question: "Este artigo foi útil?", Yes: "Sim", No: "Não", Count: "%d de %d acharam útil", Thanks: "Obrigado por nos contar.",
		Missing: "O que faltou?", MissingHint: "Opcional. Não inclua endereços de e-mail nem números.", Send: "Enviar", Sent: "Obrigado, vamos usar isso para melhorar este artigo."},
	"nl": {Question: "Was dit artikel nuttig?", Yes: "Ja", No: "Nee", Count: "%d van de %d vonden dit nuttig", Thanks: "Bedankt dat u het laat weten.",
		Missing: "Wat ontbrak er?", MissingHint: "Optioneel. Laat e-mailadressen en nummers weg.", Send: "Versturen", Sent: "Bedankt, we gebruiken het om dit artikel te verbeteren."},
	"ja": {Question: "この記事は役に立ちましたか?", Yes: "はい", No: "いいえ", Count: "%[2]d人中%[1]d人が役に立ったと答えています", Thanks: "ご回答ありがとうございます。",
		Missing: "何が足りませんでしたか?", MissingHint: "任意です。メールアドレスや番号は書かないでください。", Send: "送信", Sent: "ありがとうございます。この記事の改善に使わせていただきます。"},
	"zh-cn": {Question: "这篇文章有帮助吗？", Yes: "有", No: "没有", Count: "%[2]d 人中有 %[1]d 人觉得有帮助", Thanks: "感谢您的反馈。",
		Missing: "缺少了什么？", MissingHint: "选填。请不要填写邮箱地址或号码。", Send: "发送", Sent: "谢谢，我们会据此改进这篇文章。"},
	"ko": {Question: "이 문서가 도움이 되었나요?", Yes: "예", No: "아니요", Count: "%[2]d명 중 %[1]d명이 도움이 되었다고 답했습니다", Thanks: "알려 주셔서 감사합니다.",
		Missing: "무엇이 부족했나요?", MissingHint: "선택 사항입니다. 이메일 주소와 번호는 적지 마세요.", Send: "보내기", Sent: "감사합니다. 이 문서를 개선하는 데 쓰겠습니다."},
	"vi": {Question: "Bài viết này có hữu ích không?", Yes: "Có", No: "Không", Count: "%d trên %d người thấy hữu ích", Thanks: "Cảm ơn bạn đã cho chúng tôi biết.",
		Missing: "Bài viết còn thiếu gì?", MissingHint: "Không bắt buộc. Vui lòng không ghi địa chỉ email hay số.", Send: "Gửi", Sent: "Cảm ơn bạn, chúng tôi sẽ dùng góp ý này để cải thiện bài viết."},
	"pl": {Question: "Czy ten artykuł był pomocny?", Yes: "Tak", No: "Nie", Count: "%d z %d uznało go za pomocny", Thanks: "Dziękujemy za informację.",
		Missing: "Czego zabrakło?", MissingHint: "Opcjonalnie. Nie podawaj adresów e-mail ani numerów.", Send: "Wyślij", Sent: "Dziękujemy, wykorzystamy to, aby ulepszyć ten artykuł."},
	"sv": {Question: "Var den här artikeln till hjälp?", Yes: "Ja", No: "Nej", Count: "%d av %d tyckte att den var till hjälp", Thanks: "Tack för att du berättade.",
		Missing: "Vad saknades?", MissingHint: "Valfritt. Skriv inga e-postadresser eller nummer.", Send: "Skicka", Sent: "Tack, vi använder det för att förbättra artikeln."},
	"ru": {Question: "Была ли статья полезной?", Yes: "Да", No: "Нет", Count: "Полезной её сочли: %d из %d", Thanks: "Спасибо, что сообщили.",
		Missing: "Чего не хватило?", MissingHint: "Необязательно. Не указывайте адреса электронной почты и номера.", Send: "Отправить", Sent: "Спасибо, мы используем это, чтобы улучшить статью."},
}

// voteTextFor is the box's words for locale, in the language the rest of the page uses.
func voteTextFor(locale string) voteText {
	if text, ok := voteTexts[textKey(locale)]; ok {
		return text
	}
	return voteTexts["en"]
}
