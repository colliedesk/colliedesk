package helpcenter

import "strings"

// uiText is the help center's own words (not the knowledge base's) in one language. The fields
// with a verb (%s, %d) are formats. A page shows them in its own language: the primary language on its
// pages, a translation's language on its pages; a language without a set uses English (owner decision
// 2026-11-05: built-in sets for common languages, labels an admin edits are a later change).
type uiText struct {
	SkipToContent, SearchArticles, SearchButton, SearchForAnswer, DescribeQuestion, HowCanWeHelp, PreferToAsk string
	BrowseByTopic, Sections, Articles, Previous, Next, Pages, Updated, In, RelatedArticles                    string
	TryFewer, BrowseTopics, NotFound, NotFoundMoved, GoToStart, NothingPublished, Home, Breadcrumb            string
	Language, Search                                                                                          string
	// Formats.
	SectionsOf, ArticleOne, ArticlesN, AllN, Open, PageOf, InCollection, ResultOne, ResultsN, NoResults, SearchFor string
	// DateFormat is a Go time layout; months are written in English only by the English set.
	DateFormat string
	// AskAI names the help chat's link and button in the page's language; "" (English) keeps the label
	// the chat channel gives.
	AskAI string
	// ReadIn leads from a language with nothing published to the primary one, named after it.
	ReadIn string
}

var uiTexts = map[string]uiText{
	"en": {ReadIn: "Meanwhile, read the help center in", SkipToContent: "Skip to content", SearchArticles: "Search articles", SearchButton: "Search", SearchForAnswer: "Search for an answer",
		DescribeQuestion: "Describe your question", HowCanWeHelp: "How can we help?", PreferToAsk: "Prefer to ask in your own words?",
		BrowseByTopic: "Browse by topic", Sections: "Sections", Articles: "Articles", Previous: "Previous", Next: "Next", Pages: "Pages", Updated: "Updated", In: "in",
		RelatedArticles: "Related articles", TryFewer: "Try fewer words, another spelling, or", BrowseTopics: "browse the topics",
		NotFound: "This page does not exist", NotFoundMoved: "The article may have moved or been unpublished. Search for it, or", GoToStart: "go to the start page",
		NothingPublished: "Nothing is published here yet. Check back soon.", Home: "Home", Breadcrumb: "Breadcrumb", Language: "Language", Search: "Search",
		SectionsOf: "Sections of %s", ArticleOne: "1 article", ArticlesN: "%d articles", AllN: "All %s", Open: "Open %s", PageOf: "page %d of %d", InCollection: "In %s",
		ResultOne: "%d result for “%s”", ResultsN: "%d results for “%s”", NoResults: "No results for “%s”", SearchFor: "Search: %s", DateFormat: "2 Jan 2006"},
	"fr": {ReadIn: "En attendant, lisez le centre d’aide en", AskAI: "Demander à l’IA", SkipToContent: "Aller au contenu", SearchArticles: "Rechercher des articles", SearchButton: "Rechercher", SearchForAnswer: "Rechercher une réponse",
		DescribeQuestion: "Décrivez votre question", HowCanWeHelp: "Comment pouvons-nous vous aider ?", PreferToAsk: "Vous préférez poser la question avec vos mots ?",
		BrowseByTopic: "Parcourir par thème", Sections: "Sections", Articles: "Articles", Previous: "Précédent", Next: "Suivant", Pages: "Pages", Updated: "Mis à jour le", In: "dans",
		RelatedArticles: "Articles liés", TryFewer: "Essayez moins de mots, une autre orthographe, ou", BrowseTopics: "parcourez les thèmes",
		NotFound: "Cette page n’existe pas", NotFoundMoved: "L’article a peut-être été déplacé ou dépublié. Recherchez-le, ou", GoToStart: "revenez à l’accueil",
		NothingPublished: "Rien n’est encore publié ici. Revenez bientôt.", Home: "Accueil", Breadcrumb: "Fil d’Ariane", Language: "Langue", Search: "Recherche",
		SectionsOf: "Sections de %s", ArticleOne: "1 article", ArticlesN: "%d articles", AllN: "Voir les %s", Open: "Ouvrir %s", PageOf: "page %d sur %d", InCollection: "Dans %s",
		ResultOne: "%d résultat pour « %s »", ResultsN: "%d résultats pour « %s »", NoResults: "Aucun résultat pour « %s »", SearchFor: "Recherche : %s", DateFormat: "02/01/2006"},
	"de": {ReadIn: "Lesen Sie das Hilfe-Center inzwischen auf", AskAI: "KI fragen", SkipToContent: "Zum Inhalt springen", SearchArticles: "Artikel durchsuchen", SearchButton: "Suchen", SearchForAnswer: "Nach einer Antwort suchen",
		DescribeQuestion: "Beschreiben Sie Ihre Frage", HowCanWeHelp: "Wie können wir helfen?", PreferToAsk: "Lieber in eigenen Worten fragen?",
		BrowseByTopic: "Nach Thema stöbern", Sections: "Bereiche", Articles: "Artikel", Previous: "Zurück", Next: "Weiter", Pages: "Seiten", Updated: "Aktualisiert am", In: "in",
		RelatedArticles: "Ähnliche Artikel", TryFewer: "Versuchen Sie weniger Wörter, eine andere Schreibweise oder", BrowseTopics: "stöbern Sie in den Themen",
		NotFound: "Diese Seite gibt es nicht", NotFoundMoved: "Der Artikel wurde vielleicht verschoben oder nicht mehr veröffentlicht. Suchen Sie danach oder", GoToStart: "gehen Sie zur Startseite",
		NothingPublished: "Hier ist noch nichts veröffentlicht. Schauen Sie bald wieder vorbei.", Home: "Start", Breadcrumb: "Brotkrümelnavigation", Language: "Sprache", Search: "Suche",
		SectionsOf: "Bereiche von %s", ArticleOne: "1 Artikel", ArticlesN: "%d Artikel", AllN: "Alle %s", Open: "%s öffnen", PageOf: "Seite %d von %d", InCollection: "In %s",
		ResultOne: "%d Ergebnis für „%s“", ResultsN: "%d Ergebnisse für „%s“", NoResults: "Keine Ergebnisse für „%s“", SearchFor: "Suche: %s", DateFormat: "02.01.2006"},
	"es": {ReadIn: "Mientras tanto, lea el centro de ayuda en", AskAI: "Preguntar a la IA", SkipToContent: "Ir al contenido", SearchArticles: "Buscar artículos", SearchButton: "Buscar", SearchForAnswer: "Busca una respuesta",
		DescribeQuestion: "Describe tu pregunta", HowCanWeHelp: "¿Cómo podemos ayudarte?", PreferToAsk: "¿Prefieres preguntar con tus palabras?",
		BrowseByTopic: "Explorar por tema", Sections: "Secciones", Articles: "Artículos", Previous: "Anterior", Next: "Siguiente", Pages: "Páginas", Updated: "Actualizado el", In: "en",
		RelatedArticles: "Artículos relacionados", TryFewer: "Prueba con menos palabras, otra ortografía o", BrowseTopics: "explora los temas",
		NotFound: "Esta página no existe", NotFoundMoved: "Puede que el artículo se haya movido o despublicado. Búscalo o", GoToStart: "ve a la página de inicio",
		NothingPublished: "Aún no hay nada publicado. Vuelve pronto.", Home: "Inicio", Breadcrumb: "Ruta de navegación", Language: "Idioma", Search: "Búsqueda",
		SectionsOf: "Secciones de %s", ArticleOne: "1 artículo", ArticlesN: "%d artículos", AllN: "Ver los %s", Open: "Abrir %s", PageOf: "página %d de %d", InCollection: "En %s",
		ResultOne: "%d resultado para «%s»", ResultsN: "%d resultados para «%s»", NoResults: "No hay resultados para «%s»", SearchFor: "Búsqueda: %s", DateFormat: "02/01/2006"},
	"it": {ReadIn: "Nel frattempo, leggi il centro assistenza in", AskAI: "Chiedi all'IA", SkipToContent: "Vai al contenuto", SearchArticles: "Cerca negli articoli", SearchButton: "Cerca", SearchForAnswer: "Cerca una risposta",
		DescribeQuestion: "Descrivi la tua domanda", HowCanWeHelp: "Come possiamo aiutarti?", PreferToAsk: "Preferisci chiedere con parole tue?",
		BrowseByTopic: "Sfoglia per argomento", Sections: "Sezioni", Articles: "Articoli", Previous: "Precedente", Next: "Successiva", Pages: "Pagine", Updated: "Aggiornato il", In: "in",
		RelatedArticles: "Articoli correlati", TryFewer: "Prova con meno parole, un'altra grafia oppure", BrowseTopics: "sfoglia gli argomenti",
		NotFound: "Questa pagina non esiste", NotFoundMoved: "L'articolo potrebbe essere stato spostato o ritirato. Cercalo oppure", GoToStart: "torna alla pagina iniziale",
		NothingPublished: "Qui non è ancora pubblicato nulla. Torna presto.", Home: "Home", Breadcrumb: "Percorso", Language: "Lingua", Search: "Ricerca",
		SectionsOf: "Sezioni di %s", ArticleOne: "1 articolo", ArticlesN: "%d articoli", AllN: "Tutti i %s", Open: "Apri %s", PageOf: "pagina %d di %d", InCollection: "In %s",
		ResultOne: "%d risultato per «%s»", ResultsN: "%d risultati per «%s»", NoResults: "Nessun risultato per «%s»", SearchFor: "Ricerca: %s", DateFormat: "02/01/2006"},
	"pt": {ReadIn: "Entretanto, leia o centro de ajuda em", AskAI: "Perguntar à IA", SkipToContent: "Saltar para o conteúdo", SearchArticles: "Pesquisar artigos", SearchButton: "Pesquisar", SearchForAnswer: "Procure uma resposta",
		DescribeQuestion: "Descreva a sua pergunta", HowCanWeHelp: "Como podemos ajudar?", PreferToAsk: "Prefere perguntar por palavras suas?",
		BrowseByTopic: "Explorar por tema", Sections: "Secções", Articles: "Artigos", Previous: "Anterior", Next: "Seguinte", Pages: "Páginas", Updated: "Atualizado a", In: "em",
		RelatedArticles: "Artigos relacionados", TryFewer: "Tente menos palavras, outra ortografia, ou", BrowseTopics: "explore os temas",
		NotFound: "Esta página não existe", NotFoundMoved: "O artigo pode ter sido movido ou despublicado. Procure-o, ou", GoToStart: "volte à página inicial",
		NothingPublished: "Ainda não há nada publicado. Volte em breve.", Home: "Início", Breadcrumb: "Navegação estrutural", Language: "Idioma", Search: "Pesquisa",
		SectionsOf: "Secções de %s", ArticleOne: "1 artigo", ArticlesN: "%d artigos", AllN: "Ver os %s", Open: "Abrir %s", PageOf: "página %d de %d", InCollection: "Em %s",
		ResultOne: "%d resultado para «%s»", ResultsN: "%d resultados para «%s»", NoResults: "Sem resultados para «%s»", SearchFor: "Pesquisa: %s", DateFormat: "02/01/2006"},
	"pt-br": {ReadIn: "Enquanto isso, leia a central de ajuda em", AskAI: "Perguntar à IA", SkipToContent: "Pular para o conteúdo", SearchArticles: "Pesquisar artigos", SearchButton: "Pesquisar", SearchForAnswer: "Procure uma resposta",
		DescribeQuestion: "Descreva sua pergunta", HowCanWeHelp: "Como podemos ajudar?", PreferToAsk: "Prefere perguntar com suas palavras?",
		BrowseByTopic: "Navegar por tema", Sections: "Seções", Articles: "Artigos", Previous: "Anterior", Next: "Próxima", Pages: "Páginas", Updated: "Atualizado em", In: "em",
		RelatedArticles: "Artigos relacionados", TryFewer: "Tente menos palavras, outra grafia ou", BrowseTopics: "navegue pelos temas",
		NotFound: "Esta página não existe", NotFoundMoved: "O artigo pode ter sido movido ou despublicado. Pesquise por ele ou", GoToStart: "volte para a página inicial",
		NothingPublished: "Ainda não há nada publicado. Volte em breve.", Home: "Início", Breadcrumb: "Trilha de navegação", Language: "Idioma", Search: "Pesquisa",
		SectionsOf: "Seções de %s", ArticleOne: "1 artigo", ArticlesN: "%d artigos", AllN: "Ver os %s", Open: "Abrir %s", PageOf: "página %d de %d", InCollection: "Em %s",
		ResultOne: "%d resultado para “%s”", ResultsN: "%d resultados para “%s”", NoResults: "Nenhum resultado para “%s”", SearchFor: "Pesquisa: %s", DateFormat: "02/01/2006"},
	"nl": {ReadIn: "Lees het helpcentrum intussen in het", AskAI: "Vraag het AI", SkipToContent: "Naar de inhoud", SearchArticles: "Artikelen zoeken", SearchButton: "Zoeken", SearchForAnswer: "Zoek een antwoord",
		DescribeQuestion: "Beschrijf je vraag", HowCanWeHelp: "Hoe kunnen we helpen?", PreferToAsk: "Liever in je eigen woorden vragen?",
		BrowseByTopic: "Bladeren per onderwerp", Sections: "Secties", Articles: "Artikelen", Previous: "Vorige", Next: "Volgende", Pages: "Pagina's", Updated: "Bijgewerkt op", In: "in",
		RelatedArticles: "Gerelateerde artikelen", TryFewer: "Probeer minder woorden, een andere spelling of", BrowseTopics: "blader door de onderwerpen",
		NotFound: "Deze pagina bestaat niet", NotFoundMoved: "Het artikel is misschien verplaatst of niet meer gepubliceerd. Zoek ernaar of", GoToStart: "ga naar de startpagina",
		NothingPublished: "Hier is nog niets gepubliceerd. Kom binnenkort terug.", Home: "Start", Breadcrumb: "Kruimelpad", Language: "Taal", Search: "Zoeken",
		SectionsOf: "Secties van %s", ArticleOne: "1 artikel", ArticlesN: "%d artikelen", AllN: "Alle %s", Open: "%s openen", PageOf: "pagina %d van %d", InCollection: "In %s",
		ResultOne: "%d resultaat voor ‘%s’", ResultsN: "%d resultaten voor ‘%s’", NoResults: "Geen resultaten voor ‘%s’", SearchFor: "Zoeken: %s", DateFormat: "02-01-2006"},
	"ja": {ReadIn: "それまでは、次の言語でヘルプセンターをご覧ください:", AskAI: "AIに質問", SkipToContent: "本文へ移動", SearchArticles: "記事を検索", SearchButton: "検索", SearchForAnswer: "答えを検索",
		DescribeQuestion: "質問を入力してください", HowCanWeHelp: "どのようなご用件ですか？", PreferToAsk: "ご自分の言葉で質問したいですか？",
		BrowseByTopic: "トピックから探す", Sections: "セクション", Articles: "記事", Previous: "前へ", Next: "次へ", Pages: "ページ", Updated: "更新日", In: "カテゴリ:",
		RelatedArticles: "関連記事", TryFewer: "語数を減らすか、別の表記を試すか、", BrowseTopics: "トピックをご覧ください",
		NotFound: "このページは存在しません", NotFoundMoved: "記事は移動されたか、公開が終了した可能性があります。検索するか、", GoToStart: "トップページへ戻ってください",
		NothingPublished: "まだ公開されている記事はありません。", Home: "ホーム", Breadcrumb: "パンくずリスト", Language: "言語", Search: "検索",
		SectionsOf: "%s のセクション", ArticleOne: "1 件の記事", ArticlesN: "%d 件の記事", AllN: "%sをすべて表示", Open: "%s を開く", PageOf: "%d / %d ページ", InCollection: "%s の記事",
		ResultOne: "「%[2]s」の検索結果 %[1]d 件", ResultsN: "「%[2]s」の検索結果 %[1]d 件", NoResults: "「%s」に一致する結果はありません", SearchFor: "検索: %s", DateFormat: "2006/01/02"},
	"zh-cn": {ReadIn: "在此期间，请阅读以下语言的帮助中心：", AskAI: "问 AI", SkipToContent: "跳到正文", SearchArticles: "搜索文章", SearchButton: "搜索", SearchForAnswer: "搜索答案",
		DescribeQuestion: "描述您的问题", HowCanWeHelp: "有什么可以帮您？", PreferToAsk: "想用自己的话提问？",
		BrowseByTopic: "按主题浏览", Sections: "分类", Articles: "文章", Previous: "上一页", Next: "下一页", Pages: "页面", Updated: "更新于", In: "，分类：",
		RelatedArticles: "相关文章", TryFewer: "请尝试减少关键词、换一种写法，或", BrowseTopics: "浏览主题",
		NotFound: "此页面不存在", NotFoundMoved: "文章可能已移动或取消发布。请搜索，或", GoToStart: "返回首页",
		NothingPublished: "这里还没有发布任何内容，请稍后再来。", Home: "首页", Breadcrumb: "导航路径", Language: "语言", Search: "搜索",
		SectionsOf: "%s 的分类", ArticleOne: "1 篇文章", ArticlesN: "%d 篇文章", AllN: "全部 %s", Open: "打开 %s", PageOf: "第 %d 页，共 %d 页", InCollection: "%s 中的文章",
		ResultOne: "“%[2]s”的搜索结果：%[1]d 条", ResultsN: "“%[2]s”的搜索结果：%[1]d 条", NoResults: "没有与“%s”相关的结果", SearchFor: "搜索：%s", DateFormat: "2006-01-02"},
	"ko": {ReadIn: "그동안 다음 언어로 도움말 센터를 읽어 보세요:", AskAI: "AI에게 묻기", SkipToContent: "본문으로 건너뛰기", SearchArticles: "문서 검색", SearchButton: "검색", SearchForAnswer: "답변 검색",
		DescribeQuestion: "질문을 입력하세요", HowCanWeHelp: "무엇을 도와드릴까요?", PreferToAsk: "직접 질문하고 싶으신가요?",
		BrowseByTopic: "주제별 보기", Sections: "섹션", Articles: "문서", Previous: "이전", Next: "다음", Pages: "페이지", Updated: "업데이트:", In: "·",
		RelatedArticles: "관련 문서", TryFewer: "검색어를 줄이거나 다른 철자를 사용하거나", BrowseTopics: "주제를 둘러보세요",
		NotFound: "이 페이지는 존재하지 않습니다", NotFoundMoved: "문서가 이동되었거나 게시가 중단되었을 수 있습니다. 검색하거나", GoToStart: "첫 페이지로 이동하세요",
		NothingPublished: "아직 게시된 내용이 없습니다. 곧 다시 확인해 주세요.", Home: "홈", Breadcrumb: "탐색 경로", Language: "언어", Search: "검색",
		SectionsOf: "%s의 섹션", ArticleOne: "문서 1개", ArticlesN: "문서 %d개", AllN: "%s 모두 보기", Open: "%s 열기", PageOf: "%d / %d 페이지", InCollection: "%s의 문서",
		ResultOne: "“%[2]s” 검색 결과 %[1]d건", ResultsN: "“%[2]s” 검색 결과 %[1]d건", NoResults: "“%s”에 대한 결과가 없습니다", SearchFor: "검색: %s", DateFormat: "2006. 01. 02."},
	"vi": {ReadIn: "Trong lúc chờ, hãy đọc trung tâm trợ giúp bằng", AskAI: "Hỏi AI", SkipToContent: "Bỏ qua đến nội dung", SearchArticles: "Tìm bài viết", SearchButton: "Tìm", SearchForAnswer: "Tìm câu trả lời",
		DescribeQuestion: "Mô tả câu hỏi của bạn", HowCanWeHelp: "Chúng tôi có thể giúp gì?", PreferToAsk: "Muốn hỏi bằng lời của bạn?",
		BrowseByTopic: "Xem theo chủ đề", Sections: "Mục", Articles: "Bài viết", Previous: "Trước", Next: "Sau", Pages: "Trang", Updated: "Cập nhật", In: "trong",
		RelatedArticles: "Bài viết liên quan", TryFewer: "Thử ít từ hơn, cách viết khác, hoặc", BrowseTopics: "xem các chủ đề",
		NotFound: "Trang này không tồn tại", NotFoundMoved: "Bài viết có thể đã được chuyển hoặc ngừng xuất bản. Hãy tìm kiếm, hoặc", GoToStart: "về trang chủ",
		NothingPublished: "Chưa có nội dung nào được xuất bản. Hãy quay lại sau.", Home: "Trang chủ", Breadcrumb: "Đường dẫn", Language: "Ngôn ngữ", Search: "Tìm kiếm",
		SectionsOf: "Các mục của %s", ArticleOne: "1 bài viết", ArticlesN: "%d bài viết", AllN: "Xem tất cả %s", Open: "Mở %s", PageOf: "trang %d / %d", InCollection: "Trong %s",
		ResultOne: "%d kết quả cho “%s”", ResultsN: "%d kết quả cho “%s”", NoResults: "Không có kết quả cho “%s”", SearchFor: "Tìm kiếm: %s", DateFormat: "02/01/2006"},
	"pl": {ReadIn: "Tymczasem przeczytaj centrum pomocy w języku:", AskAI: "Zapytaj AI", SkipToContent: "Przejdź do treści", SearchArticles: "Szukaj artykułów", SearchButton: "Szukaj", SearchForAnswer: "Znajdź odpowiedź",
		DescribeQuestion: "Opisz swoje pytanie", HowCanWeHelp: "Jak możemy pomóc?", PreferToAsk: "Wolisz zapytać własnymi słowami?",
		BrowseByTopic: "Przeglądaj według tematu", Sections: "Działy", Articles: "Artykuły", Previous: "Poprzednia", Next: "Następna", Pages: "Strony", Updated: "Zaktualizowano", In: "w",
		RelatedArticles: "Powiązane artykuły", TryFewer: "Spróbuj użyć mniej słów, innej pisowni lub", BrowseTopics: "przejrzyj tematy",
		NotFound: "Ta strona nie istnieje", NotFoundMoved: "Artykuł mógł zostać przeniesiony lub wycofany. Wyszukaj go lub", GoToStart: "przejdź na stronę główną",
		NothingPublished: "Nic tu jeszcze nie opublikowano. Zajrzyj wkrótce.", Home: "Start", Breadcrumb: "Ścieżka", Language: "Język", Search: "Wyszukiwanie",
		SectionsOf: "Działy: %s", ArticleOne: "1 artykuł", ArticlesN: "Artykuły: %d", AllN: "Wszystkie (%s)", Open: "Otwórz: %s", PageOf: "strona %d z %d", InCollection: "W dziale %s",
		ResultOne: "%d wynik dla „%s”", ResultsN: "Wyniki dla „%[2]s”: %[1]d", NoResults: "Brak wyników dla „%s”", SearchFor: "Wyszukiwanie: %s", DateFormat: "02.01.2006"},
	"sv": {ReadIn: "Läs under tiden hjälpcentret på", AskAI: "Fråga AI", SkipToContent: "Hoppa till innehållet", SearchArticles: "Sök artiklar", SearchButton: "Sök", SearchForAnswer: "Sök efter ett svar",
		DescribeQuestion: "Beskriv din fråga", HowCanWeHelp: "Hur kan vi hjälpa till?", PreferToAsk: "Vill du hellre fråga med egna ord?",
		BrowseByTopic: "Bläddra efter ämne", Sections: "Avsnitt", Articles: "Artiklar", Previous: "Föregående", Next: "Nästa", Pages: "Sidor", Updated: "Uppdaterad", In: "i",
		RelatedArticles: "Relaterade artiklar", TryFewer: "Prova färre ord, en annan stavning eller", BrowseTopics: "bläddra bland ämnena",
		NotFound: "Sidan finns inte", NotFoundMoved: "Artikeln kan ha flyttats eller avpublicerats. Sök efter den eller", GoToStart: "gå till startsidan",
		NothingPublished: "Inget är publicerat här ännu. Titta in snart.", Home: "Start", Breadcrumb: "Brödsmulor", Language: "Språk", Search: "Sök",
		SectionsOf: "Avsnitt i %s", ArticleOne: "1 artikel", ArticlesN: "%d artiklar", AllN: "Alla %s", Open: "Öppna %s", PageOf: "sida %d av %d", InCollection: "I %s",
		ResultOne: "%d träff för ”%s”", ResultsN: "%d träffar för ”%s”", NoResults: "Inga träffar för ”%s”", SearchFor: "Sök: %s", DateFormat: "2006-01-02"},
	"ru": {ReadIn: "А пока читайте справочный центр на языке:", AskAI: "Спросить ИИ", SkipToContent: "Перейти к содержимому", SearchArticles: "Поиск статей", SearchButton: "Найти", SearchForAnswer: "Найдите ответ",
		DescribeQuestion: "Опишите ваш вопрос", HowCanWeHelp: "Чем мы можем помочь?", PreferToAsk: "Хотите спросить своими словами?",
		BrowseByTopic: "Темы", Sections: "Разделы", Articles: "Статьи", Previous: "Назад", Next: "Далее", Pages: "Страницы", Updated: "Обновлено", In: "в разделе",
		RelatedArticles: "Похожие статьи", TryFewer: "Попробуйте меньше слов, другое написание или", BrowseTopics: "посмотрите темы",
		NotFound: "Такой страницы нет", NotFoundMoved: "Статья могла быть перемещена или снята с публикации. Найдите её поиском или", GoToStart: "перейдите на главную",
		NothingPublished: "Здесь пока ничего не опубликовано. Загляните позже.", Home: "Главная", Breadcrumb: "Навигация", Language: "Язык", Search: "Поиск",
		SectionsOf: "Разделы: %s", ArticleOne: "Статей: 1", ArticlesN: "Статей: %d", AllN: "Все (%s)", Open: "Открыть: %s", PageOf: "страница %d из %d", InCollection: "Раздел «%s»",
		ResultOne: "Результатов по запросу «%[2]s»: %[1]d", ResultsN: "Результатов по запросу «%[2]s»: %[1]d", NoResults: "По запросу «%s» ничего не найдено", SearchFor: "Поиск: %s", DateFormat: "02.01.2006"},
}

// textFor is the set of words for locale (textKey).
func textFor(locale string) uiText { return uiTexts[textKey(locale)] }

// textKey is the set of words a locale uses, the help center's (uiTexts) and the help chat's
// (chatTexts) alike: its own, else its language's (fr-CA uses fr), else English. Chinese other than
// simplified (zh-TW, zh-Hant) gets English rather than the wrong script.
func textKey(locale string) string {
	key := strings.ToLower(locale)
	if _, ok := uiTexts[key]; ok {
		return key
	}
	if key == "zh" || key == "zh-hans" || strings.HasPrefix(key, "zh-hans-") || key == "zh-sg" {
		return "zh-cn"
	}
	if base, _, found := strings.Cut(key, "-"); found && base != "zh" {
		if _, ok := uiTexts[base]; ok {
			return base
		}
	}
	return "en"
}

// languageNames are the languages' names in themselves, for the language menu; others show their tag.
var languageNames = map[string]string{
	"en": "English", "fr": "Français", "de": "Deutsch", "es": "Español", "it": "Italiano", "pt": "Português", "pt-br": "Português (Brasil)",
	"nl": "Nederlands", "ja": "日本語", "zh-cn": "简体中文", "zh-tw": "繁體中文", "ko": "한국어", "vi": "Tiếng Việt", "pl": "Polski", "sv": "Svenska", "ru": "Русский",
	"da": "Dansk", "fi": "Suomi", "nb": "Norsk bokmål", "tr": "Türkçe", "cs": "Čeština", "uk": "Українська", "id": "Bahasa Indonesia", "th": "ไทย", "ar": "العربية",
	"he": "עברית", "hi": "हिन्दी", "el": "Ελληνικά", "hu": "Magyar", "ro": "Română",
}

func languageName(locale string) string {
	if name, ok := languageNames[strings.ToLower(locale)]; ok {
		return name
	}
	if base, _, found := strings.Cut(strings.ToLower(locale), "-"); found {
		if name, ok := languageNames[base]; ok {
			return name + " (" + locale + ")"
		}
	}
	return locale
}
