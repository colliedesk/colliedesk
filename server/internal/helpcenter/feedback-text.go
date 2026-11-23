package helpcenter

// feedbackText is the address field of "What was missing?", shown when the knowledge base opens a
// conversation from a "No" that leaves an address. The hint promises no answer: a team may not write.
type feedbackText struct {
	Email, EmailHint, InvalidEmail string
}

// maxFeedbackAddress is the longest address read, the most an address may have (RFC 5321).
const maxFeedbackAddress = 254

var feedbackTexts = map[string]feedbackText{
	"en":    {Email: "Your email address", EmailHint: "Optional. Leave it if the team may write to you about this article.", InvalidEmail: "Enter a valid email address, or leave it empty."},
	"fr":    {Email: "Votre adresse e-mail", EmailHint: "Facultatif. Indiquez-la si l’équipe peut vous écrire à propos de cet article.", InvalidEmail: "Saisissez une adresse e-mail valide, ou laissez le champ vide."},
	"de":    {Email: "Ihre E-Mail-Adresse", EmailHint: "Optional. Geben Sie sie an, wenn das Team Ihnen zu diesem Artikel schreiben darf.", InvalidEmail: "Geben Sie eine gültige E-Mail-Adresse ein oder lassen Sie das Feld leer."},
	"es":    {Email: "Tu correo electrónico", EmailHint: "Opcional. Déjalo si el equipo puede escribirte sobre este artículo.", InvalidEmail: "Escribe un correo electrónico válido o deja el campo vacío."},
	"it":    {Email: "Il tuo indirizzo email", EmailHint: "Facoltativo. Lascialo se il team può scriverti su questo articolo.", InvalidEmail: "Inserisci un indirizzo email valido o lascia vuoto il campo."},
	"pt":    {Email: "O seu endereço de email", EmailHint: "Opcional. Deixe-o se a equipa lhe puder escrever sobre este artigo.", InvalidEmail: "Introduza um endereço de email válido ou deixe o campo vazio."},
	"pt-br": {Email: "Seu endereço de e-mail", EmailHint: "Opcional. Deixe-o se a equipe puder escrever para você sobre este artigo.", InvalidEmail: "Digite um endereço de e-mail válido ou deixe o campo vazio."},
	"nl":    {Email: "Uw e-mailadres", EmailHint: "Optioneel. Vul het in als het team u over dit artikel mag schrijven.", InvalidEmail: "Vul een geldig e-mailadres in of laat het veld leeg."},
	"ja":    {Email: "メールアドレス", EmailHint: "任意です。この記事についてチームから連絡してもよい場合に入力してください。", InvalidEmail: "正しいメールアドレスを入力するか、空欄にしてください。"},
	"zh-cn": {Email: "您的邮箱地址", EmailHint: "选填。如果团队可以就这篇文章联系您，请填写。", InvalidEmail: "请输入有效的邮箱地址，或留空。"},
	"ko":    {Email: "이메일 주소", EmailHint: "선택 사항입니다. 이 문서에 대해 팀이 연락해도 된다면 적어 주세요.", InvalidEmail: "올바른 이메일 주소를 입력하거나 비워 두세요."},
	"vi":    {Email: "Địa chỉ email của bạn", EmailHint: "Không bắt buộc. Ghi lại nếu đội hỗ trợ có thể viết cho bạn về bài viết này.", InvalidEmail: "Nhập một địa chỉ email hợp lệ, hoặc để trống."},
	"pl":    {Email: "Twój adres e-mail", EmailHint: "Opcjonalnie. Podaj go, jeśli zespół może napisać do Ciebie w sprawie tego artykułu.", InvalidEmail: "Podaj prawidłowy adres e-mail albo zostaw pole puste."},
	"sv":    {Email: "Din e-postadress", EmailHint: "Valfritt. Fyll i den om teamet får skriva till dig om artikeln.", InvalidEmail: "Ange en giltig e-postadress eller lämna fältet tomt."},
	"ru":    {Email: "Ваш адрес электронной почты", EmailHint: "Необязательно. Укажите его, если команда может написать вам об этой статье.", InvalidEmail: "Введите действительный адрес электронной почты или оставьте поле пустым."},
}

// feedbackTextFor is the address field's words for locale, in the language the rest of the page uses.
func feedbackTextFor(locale string) feedbackText {
	if text, ok := feedbackTexts[textKey(locale)]; ok {
		return text
	}
	return feedbackTexts["en"]
}
