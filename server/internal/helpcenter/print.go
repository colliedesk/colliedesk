package helpcenter

import (
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// printSuffix follows an article's address for its print page: the article alone, without the header,
// the rail, the footer or the chat, so a reader prints it with the browser's own command. The help
// center runs no script, so the page cannot open the print dialog itself.
const printSuffix = "/print"

// printText is the print page's words in one language: the article's link to it, the way back and how
// to print. A language without a set uses its language's or English (textKey).
type printText struct {
	Link, Back, Hint string
}

var printTexts = map[string]printText{
	"en":    {Link: "Print", Back: "Back to the article", Hint: "Use your browser’s print command to print it or save it as a PDF."},
	"fr":    {Link: "Imprimer", Back: "Retour à l’article", Hint: "Utilisez la commande d’impression de votre navigateur pour l’imprimer ou l’enregistrer en PDF."},
	"de":    {Link: "Drucken", Back: "Zurück zum Artikel", Hint: "Mit der Druckfunktion Ihres Browsers drucken Sie ihn oder speichern ihn als PDF."},
	"es":    {Link: "Imprimir", Back: "Volver al artículo", Hint: "Usa la opción de imprimir de tu navegador para imprimirlo o guardarlo como PDF."},
	"it":    {Link: "Stampa", Back: "Torna all'articolo", Hint: "Usa il comando di stampa del browser per stamparlo o salvarlo in PDF."},
	"pt":    {Link: "Imprimir", Back: "Voltar ao artigo", Hint: "Use o comando de impressão do navegador para o imprimir ou guardar em PDF."},
	"pt-br": {Link: "Imprimir", Back: "Voltar para o artigo", Hint: "Use o comando de impressão do navegador para imprimir ou salvar em PDF."},
	"nl":    {Link: "Afdrukken", Back: "Terug naar het artikel", Hint: "Gebruik de afdrukopdracht van uw browser om het af te drukken of als pdf op te slaan."},
	"ja":    {Link: "印刷", Back: "記事に戻る", Hint: "ブラウザの印刷機能で印刷するか、PDFとして保存してください。"},
	"zh-cn": {Link: "打印", Back: "返回文章", Hint: "请使用浏览器的打印功能打印，或另存为 PDF。"},
	"ko":    {Link: "인쇄", Back: "문서로 돌아가기", Hint: "브라우저의 인쇄 기능으로 인쇄하거나 PDF로 저장하세요."},
	"vi":    {Link: "In", Back: "Quay lại bài viết", Hint: "Dùng lệnh in của trình duyệt để in hoặc lưu thành PDF."},
	"pl":    {Link: "Drukuj", Back: "Wróć do artykułu", Hint: "Użyj polecenia drukowania w przeglądarce, aby go wydrukować lub zapisać jako PDF."},
	"sv":    {Link: "Skriv ut", Back: "Tillbaka till artikeln", Hint: "Använd webbläsarens utskriftskommando för att skriva ut den eller spara den som PDF."},
	"ru":    {Link: "Печать", Back: "Вернуться к статье", Hint: "Используйте команду печати в браузере, чтобы распечатать статью или сохранить её в PDF."},
}

// printTextFor is the print page's words for locale, in the language the rest of the page uses.
func printTextFor(locale string) printText {
	if text, ok := printTexts[textKey(locale)]; ok {
		return text
	}
	return printTexts["en"]
}

// articlePrint sends the print page of article. It is not for search engines: its canonical address
// is the article's, and the sitemap leaves it out.
func (s *Site) articlePrint(response http.ResponseWriter, request *http.Request, v view, article domain.PublicKBArticle) {
	rendered := s.renders.article(v.site.WorkspaceID, article.Number, v.base, textKey(v.locale()), article.BodyMD, article.Videos...)
	data := s.page(v, article.Title, nil)
	// No masthead, footer or chat: the layout shows them only with a home link or the chat box.
	data.Home, data.AskLink = "", ""
	data.Heading, data.Body, data.NoIndex = article.Title, rendered.body, true
	data.Article = v.root + ArticlePath(article.Number, article.Slug)
	data.Canonical = v.rootAddress + ArticlePath(article.Number, article.Slug)
	data.Updated, data.UpdatedISO = article.UpdatedAt.Format(v.text.DateFormat), article.UpdatedAt.Format(time.RFC3339)
	data.Print = printTextFor(v.locale())
	response.Header().Set("X-Robots-Tag", "noindex")
	if rendered.plain {
		response.Header().Del("ETag")
		response.Header().Set("Cache-Control", "no-store")
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("print", data))
}
