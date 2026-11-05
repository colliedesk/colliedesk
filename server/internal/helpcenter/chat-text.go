package helpcenter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// ChatText is the help chat's own words in one language: the chat page, an article's chat box and the
// widget on a company's site (chat.js, page.html, widget.js). It has a set for every language uiText
// has, and a page finds it the same way (textKey): the chat speaks the language of the page around it.
// A {name}-style placeholder is filled by the script, so a word keeps the English one's placeholders
// (TestTheChatHasEveryWordInEveryLanguageTheHelpCenterHas). A plural is written the way the language's
// set writes it ("Вопросов: {n}"), with a separate word for one where English has one.
type ChatText struct {
	AITag                  string `json:"aiTag"`
	AITagTitle             string `json:"aiTagTitle"`
	BackToHelpCenter       string `json:"backToHelpCenter"`
	AskName                string `json:"askName"`
	TalkToPerson           string `json:"talkToPerson"`
	MessageAgain           string `json:"messageAgain"`
	NewChat                string `json:"newChat"`
	AskAboutArticle        string `json:"askAboutArticle"`
	ThisArticle            string `json:"thisArticle"`
	OpenFullChat           string `json:"openFullChat"`
	OpenFullChatTab        string `json:"openFullChatTab"`
	CloseChat              string `json:"closeChat"`
	AskingAbout            string `json:"askingAbout"`
	StopAskingAbout        string `json:"stopAskingAbout"`
	NoLongerAbout          string `json:"noLongerAbout"`
	AskQuestion            string `json:"askQuestion"`
	AskQuestionAbout       string `json:"askQuestionAbout"`
	SendQuestion           string `json:"sendQuestion"`
	YourQuestion           string `json:"yourQuestion"`
	NoteShort              string `json:"noteShort"`
	NoteLong               string `json:"noteLong"`
	Shorten                string `json:"shorten"`
	Suggested              string `json:"suggested"`
	HowCanWeHelp           string `json:"howCanWeHelp"`
	Assistant              string `json:"assistant"`
	QuestionAndAnswer      string `json:"questionAndAnswer"`
	YouAsked               string `json:"youAsked"`
	SourceOf               string `json:"sourceOf"`
	Source                 string `json:"source"`
	Sources                string `json:"sources"`
	Helpful                string `json:"helpful"`
	NotHelpful             string `json:"notHelpful"`
	Thanks                 string `json:"thanks"`
	CopyAnswer             string `json:"copyAnswer"`
	Copied                 string `json:"copied"`
	Paused                 string `json:"paused"`
	PausedMatches          string `json:"pausedMatches"`
	StillRead              string `json:"stillRead"`
	NotInArticles          string `json:"notInArticles"`
	ContactsMissing        string `json:"contactsMissing"`
	TeamContacts           string `json:"teamContacts"`
	WentWrong              string `json:"wentWrong"`
	DidNotFinish           string `json:"didNotFinish"`
	TryAgain               string `json:"tryAgain"`
	OnItsWay               string `json:"onItsWay"`
	TooMany                string `json:"tooMany"`
	NotSent                string `json:"notSent"`
	CouldNotSend           string `json:"couldNotSend"`
	CheckConnection        string `json:"checkConnection"`
	ReadyOne               string `json:"readyOne"`
	ReadyN                 string `json:"readyN"`
	PausedShort            string `json:"pausedShort"`
	ReadyNotFound          string `json:"readyNotFound"`
	Reconnecting           string `json:"reconnecting"`
	Reconnected            string `json:"reconnected"`
	AskAgainIn             string `json:"askAgainIn"`
	AskAgain               string `json:"askAgain"`
	Unavailable            string `json:"unavailable"`
	GoToHelpCenter         string `json:"goToHelpCenter"`
	ChatLong               string `json:"chatLong"`
	StartNewToKeep         string `json:"startNewToKeep"`
	ContinueTitle          string `json:"continueTitle"`
	QuestionsOne           string `json:"questionsOne"`
	QuestionsN             string `json:"questionsN"`
	ActiveOne              string `json:"activeOne"`
	ActiveN                string `json:"activeN"`
	Resume                 string `json:"resume"`
	Continue               string `json:"continue"`
	PreviousOpened         string `json:"previousOpened"`
	ChooseFirst            string `json:"chooseFirst"`
	NewStarted             string `json:"newStarted"`
	StartNewTitle          string `json:"startNewTitle"`
	ClearedHere            string `json:"clearedHere"`
	Cancel                 string `json:"cancel"`
	StartNew               string `json:"startNew"`
	ContinuesInTab         string `json:"continuesInTab"`
	SendTo                 string `json:"sendTo"`
	Sending                string `json:"sending"`
	EmailInvalid           string `json:"emailInvalid"`
	TooManyRequests        string `json:"tooManyRequests"`
	CouldNotSendLater      string `json:"couldNotSendLater"`
	CouldNotSendConnection string `json:"couldNotSendConnection"`
	HandoffIntro           string `json:"handoffIntro"`
	Email                  string `json:"email"`
	Name                   string `json:"name"`
	Optional               string `json:"optional"`
	AnythingToAdd          string `json:"anythingToAdd"`
	SentHas                string `json:"sentHas"`
	SentReply              string `json:"sentReply"`
	Reference              string `json:"reference"`
	SkipToChat             string `json:"skipToChat"`
	NeedsJS                string `json:"needsJS"`
	SearchArticles         string `json:"searchArticles"`
	OrGoBackTo             string `json:"orGoBackTo"`
	WidgetLabel            string `json:"widgetLabel"`
}

var chatTexts = map[string]ChatText{
	"en": {AITag: "AI", AITagTitle: "Answers are written by AI", BackToHelpCenter: "Back to help center", AskName: "Ask {name}", TalkToPerson: "Talk to a person",
		MessageAgain: "Message the team again", NewChat: "New chat", AskAboutArticle: "Ask about this article", ThisArticle: "This article",
		OpenFullChat: "Open in full chat", OpenFullChatTab: "Open full chat in a new tab", CloseChat: "Close chat", AskingAbout: "Asking about:",
		StopAskingAbout: "Stop asking about “{title}”", NoLongerAbout: "Questions are no longer about this article.", AskQuestion: "Ask a question",
		AskQuestionAbout: "Ask a question about {name}", SendQuestion: "Send question", YourQuestion: "Your question",
		NoteShort: "AI answers can be wrong. Kept on this browser for 30 days.",
		NoteLong:  "AI answers from {name}’s help articles and can be wrong. This chat is kept on this browser for 30 days.",
		Shorten:   "Shorten your question to {limit} characters.", Suggested: "Suggested questions", HowCanWeHelp: "How can we help?",
		Assistant: "AI assistant · answers from {name}’s help articles", QuestionAndAnswer: "Question and answer", YouAsked: "You asked:",
		SourceOf: "Source {n}: {title}", Source: "Source", Sources: "Sources", Helpful: "Helpful", NotHelpful: "Not helpful", Thanks: "Thanks for the feedback",
		CopyAnswer: "Copy answer", Copied: "Copied", Paused: "AI answers are paused for now.", PausedMatches: "These articles match your question:",
		StillRead: "You can still read the help articles.", NotInArticles: "Couldn’t find this in the help articles.",
		ContactsMissing: "Some contact details were not in the articles.", TeamContacts: "The team can give you their contact details.",
		WentWrong: "Something went wrong.", DidNotFinish: "The answer did not finish.", TryAgain: "Try again", OnItsWay: "Answer on its way",
		TooMany: "Too many questions, wait a minute.", NotSent: "Your question was not sent. Ask it again when the timer ends.", CouldNotSend: "Couldn’t send.",
		CheckConnection: "Check your connection and try again. Your question is still in the box.", ReadyOne: "Answer ready, 1 source.",
		ReadyN: "Answer ready, {n} sources.", PausedShort: "AI answers are paused.", ReadyNotFound: "Answer ready. Couldn’t find this in the help articles.",
		Reconnecting: "Reconnecting…", Reconnected: "Reconnected. Answer ready.", AskAgainIn: "You can ask again in {time}", AskAgain: "You can ask again.",
		Unavailable: "AI chat isn’t available right now.", GoToHelpCenter: "Go to the help center", ChatLong: "This chat is long.",
		StartNewToKeep: "Start a new chat to keep asking.", ContinueTitle: "Continue your previous chat?", QuestionsOne: "1 question",
		QuestionsN: "{n} questions", ActiveOne: "last active 1 day ago", ActiveN: "last active {n} days ago",
		Resume: "{questions} · {active}. It stays hidden until you continue.", Continue: "Continue", PreviousOpened: "Previous chat opened.",
		ChooseFirst: "Choose Continue or New chat first", NewStarted: "New chat started.", StartNewTitle: "Start a new chat?",
		ClearedHere: "This one will be cleared from this browser.", Cancel: "Cancel", StartNew: "Start new chat",
		ContinuesInTab: "This chat continues in the new tab.", SendTo: "Send to {name}", Sending: "Sending…",
		EmailInvalid: "Enter an email address like name@example.com.", TooManyRequests: "Too many requests, try again in a minute.",
		CouldNotSendLater: "Couldn’t send. Try again in a moment.", CouldNotSendConnection: "Couldn’t send. Check your connection and try again.",
		HandoffIntro: "We send this chat to the {name} team with your note. You can keep asking the AI meanwhile.", Email: "Email", Name: "Name",
		Optional: "(optional)", AnythingToAdd: "Anything to add?", SentHas: "Sent. {name} has your message.", SentReply: "Sent. {name} will reply by email.",
		Reference: "Reference {ref}. You can keep asking here.", SkipToChat: "Skip to the chat", NeedsJS: "AI chat needs JavaScript.",
		SearchArticles: "Search the articles", OrGoBackTo: "or go back to", WidgetLabel: "AI chat: ask {name}"},
	"fr": {AITag: "IA", AITagTitle: "Les réponses sont rédigées par une IA", BackToHelpCenter: "Retour au centre d’aide", AskName: "Demander à {name}",
		TalkToPerson: "Parler à une personne", MessageAgain: "Écrire à nouveau à l’équipe", NewChat: "Nouvelle conversation",
		AskAboutArticle: "Poser une question sur cet article", ThisArticle: "Cet article", OpenFullChat: "Ouvrir en plein écran",
		OpenFullChatTab: "Ouvrir la conversation dans un nouvel onglet", CloseChat: "Fermer la conversation", AskingAbout: "Question sur :",
		StopAskingAbout: "Ne plus poser de questions sur « {title} »", NoLongerAbout: "Les questions ne portent plus sur cet article.",
		AskQuestion: "Posez une question", AskQuestionAbout: "Posez une question sur {name}", SendQuestion: "Envoyer la question",
		YourQuestion: "Votre question", NoteShort: "Les réponses de l’IA peuvent être fausses. Conservé dans ce navigateur pendant 30 jours.",
		NoteLong: "L’IA répond à partir des articles d’aide de {name} et peut se tromper. Cette conversation est conservée dans ce navigateur pendant 30 jours.",
		Shorten:  "Raccourcissez votre question à {limit} caractères.", Suggested: "Questions suggérées", HowCanWeHelp: "Comment pouvons-nous vous aider ?",
		Assistant: "Assistant IA · répond à partir des articles d’aide de {name}", QuestionAndAnswer: "Question et réponse", YouAsked: "Vous avez demandé :",
		SourceOf: "Source {n} : {title}", Source: "Source", Sources: "Sources", Helpful: "Utile", NotHelpful: "Pas utile", Thanks: "Merci pour votre retour",
		CopyAnswer: "Copier la réponse", Copied: "Copié", Paused: "Les réponses de l’IA sont suspendues pour le moment.",
		PausedMatches: "Ces articles correspondent à votre question :", StillRead: "Vous pouvez toujours lire les articles d’aide.",
		NotInArticles: "Introuvable dans les articles d’aide.", ContactsMissing: "Certaines coordonnées ne figuraient pas dans les articles.",
		TeamContacts: "L’équipe peut vous donner ses coordonnées.", WentWrong: "Une erreur s’est produite.", DidNotFinish: "La réponse ne s’est pas terminée.",
		TryAgain: "Réessayer", OnItsWay: "Réponse en cours", TooMany: "Trop de questions, patientez une minute.",
		NotSent: "Votre question n’a pas été envoyée. Posez-la à nouveau à la fin du décompte.", CouldNotSend: "Envoi impossible.",
		CheckConnection: "Vérifiez votre connexion et réessayez. Votre question est toujours dans le champ.", ReadyOne: "Réponse prête, 1 source.",
		ReadyN: "Réponse prête, {n} sources.", PausedShort: "Les réponses de l’IA sont suspendues.",
		ReadyNotFound: "Réponse prête. Introuvable dans les articles d’aide.", Reconnecting: "Reconnexion…", Reconnected: "Reconnecté. Réponse prête.",
		AskAgainIn: "Vous pourrez redemander dans {time}", AskAgain: "Vous pouvez redemander.", Unavailable: "Le chat IA n’est pas disponible pour le moment.",
		GoToHelpCenter: "Aller au centre d’aide", ChatLong: "Cette conversation est longue.",
		StartNewToKeep: "Commencez une nouvelle conversation pour continuer.", ContinueTitle: "Reprendre votre conversation précédente ?",
		QuestionsOne: "1 question", QuestionsN: "{n} questions", ActiveOne: "dernière activité il y a 1 jour", ActiveN: "dernière activité il y a {n} jours",
		Resume: "{questions} · {active}. Elle reste masquée tant que vous ne la reprenez pas.", Continue: "Reprendre",
		PreviousOpened: "Conversation précédente ouverte.", ChooseFirst: "Choisissez d’abord Reprendre ou Nouvelle conversation",
		NewStarted: "Nouvelle conversation commencée.", StartNewTitle: "Commencer une nouvelle conversation ?",
		ClearedHere: "Celle-ci sera effacée de ce navigateur.", Cancel: "Annuler", StartNew: "Nouvelle conversation",
		ContinuesInTab: "Cette conversation continue dans le nouvel onglet.", SendTo: "Envoyer à {name}", Sending: "Envoi…",
		EmailInvalid: "Saisissez une adresse e-mail comme nom@example.com.", TooManyRequests: "Trop de demandes, réessayez dans une minute.",
		CouldNotSendLater: "Envoi impossible. Réessayez dans un instant.", CouldNotSendConnection: "Envoi impossible. Vérifiez votre connexion et réessayez.",
		HandoffIntro: "Nous envoyons cette conversation à l’équipe {name} avec votre message. Vous pouvez continuer à interroger l’IA en attendant.",
		Email:        "E-mail", Name: "Nom", Optional: "(facultatif)", AnythingToAdd: "Quelque chose à ajouter ?", SentHas: "Envoyé. {name} a reçu votre message.",
		SentReply: "Envoyé. {name} vous répondra par e-mail.", Reference: "Référence {ref}. Vous pouvez continuer à poser des questions ici.",
		SkipToChat: "Aller à la conversation", NeedsJS: "Le chat IA nécessite JavaScript.", SearchArticles: "Rechercher dans les articles",
		OrGoBackTo: "ou revenir à", WidgetLabel: "Chat IA : demander à {name}"},
	"de": {AITag: "KI", AITagTitle: "Antworten werden von einer KI geschrieben", BackToHelpCenter: "Zurück zum Hilfe-Center", AskName: "{name} fragen",
		TalkToPerson: "Mit einem Menschen sprechen", MessageAgain: "Dem Team erneut schreiben", NewChat: "Neuer Chat",
		AskAboutArticle: "Zu diesem Artikel fragen", ThisArticle: "Dieser Artikel", OpenFullChat: "Im großen Chat öffnen",
		OpenFullChatTab: "Chat in einem neuen Tab öffnen", CloseChat: "Chat schließen", AskingAbout: "Frage zu:",
		StopAskingAbout: "Nicht mehr zu „{title}“ fragen", NoLongerAbout: "Fragen beziehen sich nicht mehr auf diesen Artikel.",
		AskQuestion: "Stellen Sie eine Frage", AskQuestionAbout: "Stellen Sie eine Frage zu {name}", SendQuestion: "Frage senden", YourQuestion: "Ihre Frage",
		NoteShort: "KI-Antworten können falsch sein. 30 Tage in diesem Browser gespeichert.",
		NoteLong:  "Die KI antwortet aus den Hilfeartikeln von {name} und kann sich irren. Dieser Chat wird 30 Tage in diesem Browser gespeichert.",
		Shorten:   "Kürzen Sie Ihre Frage auf {limit} Zeichen.", Suggested: "Vorgeschlagene Fragen", HowCanWeHelp: "Wie können wir helfen?",
		Assistant: "KI-Assistent · antwortet aus den Hilfeartikeln von {name}", QuestionAndAnswer: "Frage und Antwort", YouAsked: "Ihre Frage:",
		SourceOf: "Quelle {n}: {title}", Source: "Quelle", Sources: "Quellen", Helpful: "Hilfreich", NotHelpful: "Nicht hilfreich",
		Thanks: "Danke für Ihr Feedback", CopyAnswer: "Antwort kopieren", Copied: "Kopiert", Paused: "KI-Antworten sind vorübergehend pausiert.",
		PausedMatches: "Diese Artikel passen zu Ihrer Frage:", StillRead: "Sie können weiterhin die Hilfeartikel lesen.",
		NotInArticles: "In den Hilfeartikeln nicht gefunden.", ContactsMissing: "Einige Kontaktdaten standen nicht in den Artikeln.",
		TeamContacts: "Das Team kann Ihnen seine Kontaktdaten geben.", WentWrong: "Etwas ist schiefgelaufen.", DidNotFinish: "Die Antwort wurde nicht fertig.",
		TryAgain: "Erneut versuchen", OnItsWay: "Antwort ist unterwegs", TooMany: "Zu viele Fragen, bitte eine Minute warten.",
		NotSent: "Ihre Frage wurde nicht gesendet. Stellen Sie sie erneut, wenn der Timer abgelaufen ist.", CouldNotSend: "Senden fehlgeschlagen.",
		CheckConnection: "Prüfen Sie Ihre Verbindung und versuchen Sie es erneut. Ihre Frage steht noch im Feld.", ReadyOne: "Antwort fertig, 1 Quelle.",
		ReadyN: "Antwort fertig, {n} Quellen.", PausedShort: "KI-Antworten sind pausiert.",
		ReadyNotFound: "Antwort fertig. In den Hilfeartikeln nicht gefunden.", Reconnecting: "Verbindung wird wiederhergestellt…",
		Reconnected: "Wieder verbunden. Antwort fertig.", AskAgainIn: "Sie können in {time} wieder fragen", AskAgain: "Sie können wieder fragen.",
		Unavailable: "Der KI-Chat ist gerade nicht verfügbar.", GoToHelpCenter: "Zum Hilfe-Center", ChatLong: "Dieser Chat ist lang.",
		StartNewToKeep: "Starten Sie einen neuen Chat, um weiterzufragen.", ContinueTitle: "Vorherigen Chat fortsetzen?", QuestionsOne: "1 Frage",
		QuestionsN: "{n} Fragen", ActiveOne: "zuletzt aktiv vor 1 Tag", ActiveN: "zuletzt aktiv vor {n} Tagen",
		Resume: "{questions} · {active}. Er bleibt verborgen, bis Sie fortsetzen.", Continue: "Fortsetzen", PreviousOpened: "Vorheriger Chat geöffnet.",
		ChooseFirst: "Wählen Sie zuerst Fortsetzen oder Neuer Chat", NewStarted: "Neuer Chat gestartet.", StartNewTitle: "Neuen Chat starten?",
		ClearedHere: "Dieser wird aus diesem Browser gelöscht.", Cancel: "Abbrechen", StartNew: "Neuen Chat starten",
		ContinuesInTab: "Dieser Chat geht im neuen Tab weiter.", SendTo: "An {name} senden", Sending: "Wird gesendet…",
		EmailInvalid: "Geben Sie eine E-Mail-Adresse wie name@example.com ein.", TooManyRequests: "Zu viele Anfragen, versuchen Sie es in einer Minute erneut.",
		CouldNotSendLater:      "Senden fehlgeschlagen. Versuchen Sie es gleich noch einmal.",
		CouldNotSendConnection: "Senden fehlgeschlagen. Prüfen Sie Ihre Verbindung und versuchen Sie es erneut.",
		HandoffIntro:           "Wir senden diesen Chat mit Ihrer Notiz an das Team von {name}. Sie können die KI währenddessen weiter fragen.", Email: "E-Mail",
		Name: "Name", Optional: "(optional)", AnythingToAdd: "Möchten Sie etwas ergänzen?", SentHas: "Gesendet. {name} hat Ihre Nachricht.",
		SentReply: "Gesendet. {name} antwortet per E-Mail.", Reference: "Referenz {ref}. Sie können hier weiter fragen.", SkipToChat: "Zum Chat springen",
		NeedsJS: "Der KI-Chat benötigt JavaScript.", SearchArticles: "Artikel durchsuchen", OrGoBackTo: "oder zurück zu", WidgetLabel: "KI-Chat: {name} fragen"},
	"es": {AITag: "IA", AITagTitle: "Las respuestas las escribe una IA", BackToHelpCenter: "Volver al centro de ayuda", AskName: "Pregunta a {name}",
		TalkToPerson: "Hablar con una persona", MessageAgain: "Escribir de nuevo al equipo", NewChat: "Nueva conversación",
		AskAboutArticle: "Preguntar sobre este artículo", ThisArticle: "Este artículo", OpenFullChat: "Abrir en el chat completo",
		OpenFullChatTab: "Abrir el chat completo en una pestaña nueva", CloseChat: "Cerrar chat", AskingAbout: "Preguntando sobre:",
		StopAskingAbout: "Dejar de preguntar sobre «{title}»", NoLongerAbout: "Las preguntas ya no son sobre este artículo.", AskQuestion: "Haz una pregunta",
		AskQuestionAbout: "Haz una pregunta sobre {name}", SendQuestion: "Enviar pregunta", YourQuestion: "Tu pregunta",
		NoteShort: "Las respuestas de la IA pueden ser incorrectas. Se guarda en este navegador 30 días.",
		NoteLong:  "La IA responde con los artículos de ayuda de {name} y puede equivocarse. Esta conversación se guarda en este navegador 30 días.",
		Shorten:   "Acorta tu pregunta a {limit} caracteres.", Suggested: "Preguntas sugeridas", HowCanWeHelp: "¿Cómo podemos ayudarte?",
		Assistant: "Asistente de IA · responde con los artículos de ayuda de {name}", QuestionAndAnswer: "Pregunta y respuesta", YouAsked: "Preguntaste:",
		SourceOf: "Fuente {n}: {title}", Source: "Fuente", Sources: "Fuentes", Helpful: "Útil", NotHelpful: "No es útil", Thanks: "Gracias por tu opinión",
		CopyAnswer: "Copiar respuesta", Copied: "Copiado", Paused: "Las respuestas de la IA están en pausa por ahora.",
		PausedMatches: "Estos artículos coinciden con tu pregunta:", StillRead: "Puedes seguir leyendo los artículos de ayuda.",
		NotInArticles: "No se encontró en los artículos de ayuda.", ContactsMissing: "Algunos datos de contacto no estaban en los artículos.",
		TeamContacts: "El equipo puede darte sus datos de contacto.", WentWrong: "Algo salió mal.", DidNotFinish: "La respuesta no terminó.",
		TryAgain: "Reintentar", OnItsWay: "Respuesta en camino", TooMany: "Demasiadas preguntas, espera un minuto.",
		NotSent: "Tu pregunta no se envió. Vuelve a hacerla cuando termine el contador.", CouldNotSend: "No se pudo enviar.",
		CheckConnection: "Revisa tu conexión e inténtalo de nuevo. Tu pregunta sigue en el cuadro.", ReadyOne: "Respuesta lista, 1 fuente.",
		ReadyN: "Respuesta lista, {n} fuentes.", PausedShort: "Las respuestas de la IA están en pausa.",
		ReadyNotFound: "Respuesta lista. No se encontró en los artículos de ayuda.", Reconnecting: "Reconectando…", Reconnected: "Reconectado. Respuesta lista.",
		AskAgainIn: "Podrás volver a preguntar en {time}", AskAgain: "Ya puedes volver a preguntar.",
		Unavailable: "El chat con IA no está disponible ahora mismo.", GoToHelpCenter: "Ir al centro de ayuda", ChatLong: "Esta conversación es larga.",
		StartNewToKeep: "Empieza una conversación nueva para seguir preguntando.", ContinueTitle: "¿Continuar tu conversación anterior?",
		QuestionsOne: "1 pregunta", QuestionsN: "{n} preguntas", ActiveOne: "última actividad hace 1 día", ActiveN: "última actividad hace {n} días",
		Resume: "{questions} · {active}. Seguirá oculta hasta que continúes.", Continue: "Continuar", PreviousOpened: "Conversación anterior abierta.",
		ChooseFirst: "Elige primero Continuar o Nueva conversación", NewStarted: "Nueva conversación iniciada.",
		StartNewTitle: "¿Empezar una conversación nueva?", ClearedHere: "Esta se borrará de este navegador.", Cancel: "Cancelar",
		StartNew: "Empezar conversación nueva", ContinuesInTab: "Esta conversación continúa en la pestaña nueva.", SendTo: "Enviar a {name}",
		Sending: "Enviando…", EmailInvalid: "Escribe un correo electrónico como nombre@example.com.",
		TooManyRequests: "Demasiadas solicitudes, inténtalo de nuevo en un minuto.", CouldNotSendLater: "No se pudo enviar. Inténtalo de nuevo en un momento.",
		CouldNotSendConnection: "No se pudo enviar. Revisa tu conexión e inténtalo de nuevo.",
		HandoffIntro:           "Enviamos esta conversación al equipo de {name} con tu nota. Mientras tanto, puedes seguir preguntando a la IA.",
		Email:                  "Correo electrónico", Name: "Nombre", Optional: "(opcional)", AnythingToAdd: "¿Algo que añadir?", SentHas: "Enviado. {name} tiene tu mensaje.",
		SentReply: "Enviado. {name} te responderá por correo electrónico.", Reference: "Referencia {ref}. Puedes seguir preguntando aquí.",
		SkipToChat: "Ir al chat", NeedsJS: "El chat con IA necesita JavaScript.", SearchArticles: "Buscar en los artículos", OrGoBackTo: "o volver a",
		WidgetLabel: "Chat con IA: pregunta a {name}"},
	"it": {AITag: "IA", AITagTitle: "Le risposte sono scritte da un'IA", BackToHelpCenter: "Torna al centro assistenza", AskName: "Chiedi a {name}",
		TalkToPerson: "Parla con una persona", MessageAgain: "Scrivi di nuovo al team", NewChat: "Nuova chat", AskAboutArticle: "Chiedi di questo articolo",
		ThisArticle: "Questo articolo", OpenFullChat: "Apri nella chat completa", OpenFullChatTab: "Apri la chat completa in una nuova scheda",
		CloseChat: "Chiudi la chat", AskingAbout: "Domande su:", StopAskingAbout: "Smetti di chiedere di «{title}»",
		NoLongerAbout: "Le domande non riguardano più questo articolo.", AskQuestion: "Fai una domanda", AskQuestionAbout: "Fai una domanda su {name}",
		SendQuestion: "Invia la domanda", YourQuestion: "La tua domanda",
		NoteShort: "Le risposte dell'IA possono essere sbagliate. Conservata in questo browser per 30 giorni.",
		NoteLong:  "L'IA risponde dagli articoli di assistenza di {name} e può sbagliare. Questa chat è conservata in questo browser per 30 giorni.",
		Shorten:   "Accorcia la domanda a {limit} caratteri.", Suggested: "Domande suggerite", HowCanWeHelp: "Come possiamo aiutarti?",
		Assistant: "Assistente IA · risponde dagli articoli di assistenza di {name}", QuestionAndAnswer: "Domanda e risposta", YouAsked: "Hai chiesto:",
		SourceOf: "Fonte {n}: {title}", Source: "Fonte", Sources: "Fonti", Helpful: "Utile", NotHelpful: "Non utile", Thanks: "Grazie per il feedback",
		CopyAnswer: "Copia la risposta", Copied: "Copiato", Paused: "Le risposte dell'IA sono sospese per ora.",
		PausedMatches: "Questi articoli corrispondono alla tua domanda:", StillRead: "Puoi comunque leggere gli articoli di assistenza.",
		NotInArticles: "Non trovato negli articoli di assistenza.", ContactsMissing: "Alcuni recapiti non erano negli articoli.",
		TeamContacts: "Il team può darti i suoi recapiti.", WentWrong: "Qualcosa è andato storto.", DidNotFinish: "La risposta non è stata completata.",
		TryAgain: "Riprova", OnItsWay: "Risposta in arrivo", TooMany: "Troppe domande, attendi un minuto.",
		NotSent: "La tua domanda non è stata inviata. Ripetila quando il timer finisce.", CouldNotSend: "Invio non riuscito.",
		CheckConnection: "Controlla la connessione e riprova. La tua domanda è ancora nel campo.", ReadyOne: "Risposta pronta, 1 fonte.",
		ReadyN: "Risposta pronta, {n} fonti.", PausedShort: "Le risposte dell'IA sono sospese.",
		ReadyNotFound: "Risposta pronta. Non trovato negli articoli di assistenza.", Reconnecting: "Riconnessione…", Reconnected: "Riconnesso. Risposta pronta.",
		AskAgainIn: "Potrai chiedere di nuovo tra {time}", AskAgain: "Puoi chiedere di nuovo.", Unavailable: "La chat IA non è disponibile al momento.",
		GoToHelpCenter: "Vai al centro assistenza", ChatLong: "Questa chat è lunga.", StartNewToKeep: "Inizia una nuova chat per continuare a chiedere.",
		ContinueTitle: "Continuare la chat precedente?", QuestionsOne: "1 domanda", QuestionsN: "{n} domande", ActiveOne: "ultima attività 1 giorno fa",
		ActiveN: "ultima attività {n} giorni fa", Resume: "{questions} · {active}. Resta nascosta finché non continui.", Continue: "Continua",
		PreviousOpened: "Chat precedente aperta.", ChooseFirst: "Scegli prima Continua o Nuova chat", NewStarted: "Nuova chat iniziata.",
		StartNewTitle: "Iniziare una nuova chat?", ClearedHere: "Questa verrà cancellata da questo browser.", Cancel: "Annulla", StartNew: "Inizia nuova chat",
		ContinuesInTab: "Questa chat continua nella nuova scheda.", SendTo: "Invia a {name}", Sending: "Invio…",
		EmailInvalid: "Inserisci un indirizzo email come nome@example.com.", TooManyRequests: "Troppe richieste, riprova tra un minuto.",
		CouldNotSendLater: "Invio non riuscito. Riprova tra un momento.", CouldNotSendConnection: "Invio non riuscito. Controlla la connessione e riprova.",
		HandoffIntro: "Inviamo questa chat al team di {name} con la tua nota. Nel frattempo puoi continuare a chiedere all'IA.", Email: "Email", Name: "Nome",
		Optional: "(facoltativo)", AnythingToAdd: "Vuoi aggiungere qualcosa?", SentHas: "Inviato. {name} ha ricevuto il tuo messaggio.",
		SentReply: "Inviato. {name} ti risponderà via email.", Reference: "Riferimento {ref}. Puoi continuare a chiedere qui.", SkipToChat: "Vai alla chat",
		NeedsJS: "La chat IA richiede JavaScript.", SearchArticles: "Cerca negli articoli", OrGoBackTo: "oppure torna a",
		WidgetLabel: "Chat IA: chiedi a {name}"},
	"pt": {AITag: "IA", AITagTitle: "As respostas são escritas por IA", BackToHelpCenter: "Voltar ao centro de ajuda", AskName: "Perguntar a {name}",
		TalkToPerson: "Falar com uma pessoa", MessageAgain: "Escrever de novo à equipa", NewChat: "Nova conversa",
		AskAboutArticle: "Perguntar sobre este artigo", ThisArticle: "Este artigo", OpenFullChat: "Abrir na conversa completa",
		OpenFullChatTab: "Abrir a conversa completa num novo separador", CloseChat: "Fechar conversa", AskingAbout: "A perguntar sobre:",
		StopAskingAbout: "Deixar de perguntar sobre «{title}»", NoLongerAbout: "As perguntas já não são sobre este artigo.", AskQuestion: "Faça uma pergunta",
		AskQuestionAbout: "Faça uma pergunta sobre {name}", SendQuestion: "Enviar pergunta", YourQuestion: "A sua pergunta",
		NoteShort: "As respostas da IA podem estar erradas. Guardada neste navegador durante 30 dias.",
		NoteLong:  "A IA responde com os artigos de ajuda de {name} e pode errar. Esta conversa fica guardada neste navegador durante 30 dias.",
		Shorten:   "Encurte a pergunta para {limit} caracteres.", Suggested: "Perguntas sugeridas", HowCanWeHelp: "Como podemos ajudar?",
		Assistant: "Assistente de IA · responde com os artigos de ajuda de {name}", QuestionAndAnswer: "Pergunta e resposta", YouAsked: "Perguntou:",
		SourceOf: "Fonte {n}: {title}", Source: "Fonte", Sources: "Fontes", Helpful: "Útil", NotHelpful: "Não é útil", Thanks: "Obrigado pela sua opinião",
		CopyAnswer: "Copiar resposta", Copied: "Copiado", Paused: "As respostas da IA estão em pausa de momento.",
		PausedMatches: "Estes artigos correspondem à sua pergunta:", StillRead: "Pode continuar a ler os artigos de ajuda.",
		NotInArticles: "Não foi encontrado nos artigos de ajuda.", ContactsMissing: "Alguns contactos não estavam nos artigos.",
		TeamContacts: "A equipa pode dar-lhe os seus contactos.", WentWrong: "Algo correu mal.", DidNotFinish: "A resposta não terminou.",
		TryAgain: "Tentar novamente", OnItsWay: "Resposta a caminho", TooMany: "Demasiadas perguntas, aguarde um minuto.",
		NotSent: "A sua pergunta não foi enviada. Volte a fazê-la quando o temporizador terminar.", CouldNotSend: "Não foi possível enviar.",
		CheckConnection: "Verifique a ligação e tente novamente. A sua pergunta continua na caixa.", ReadyOne: "Resposta pronta, 1 fonte.",
		ReadyN: "Resposta pronta, {n} fontes.", PausedShort: "As respostas da IA estão em pausa.",
		ReadyNotFound: "Resposta pronta. Não foi encontrado nos artigos de ajuda.", Reconnecting: "A religar…",
		Reconnected: "Ligação restabelecida. Resposta pronta.", AskAgainIn: "Pode voltar a perguntar dentro de {time}", AskAgain: "Já pode voltar a perguntar.",
		Unavailable: "O chat com IA não está disponível de momento.", GoToHelpCenter: "Ir para o centro de ajuda", ChatLong: "Esta conversa é longa.",
		StartNewToKeep: "Inicie uma nova conversa para continuar a perguntar.", ContinueTitle: "Continuar a conversa anterior?", QuestionsOne: "1 pergunta",
		QuestionsN: "{n} perguntas", ActiveOne: "última atividade há 1 dia", ActiveN: "última atividade há {n} dias",
		Resume: "{questions} · {active}. Fica oculta até continuar.", Continue: "Continuar", PreviousOpened: "Conversa anterior aberta.",
		ChooseFirst: "Escolha primeiro Continuar ou Nova conversa", NewStarted: "Nova conversa iniciada.", StartNewTitle: "Iniciar uma nova conversa?",
		ClearedHere: "Esta será apagada deste navegador.", Cancel: "Cancelar", StartNew: "Iniciar nova conversa",
		ContinuesInTab: "Esta conversa continua no novo separador.", SendTo: "Enviar para {name}", Sending: "A enviar…",
		EmailInvalid: "Introduza um endereço de email como nome@example.com.", TooManyRequests: "Demasiados pedidos, tente novamente dentro de um minuto.",
		CouldNotSendLater:      "Não foi possível enviar. Tente novamente daqui a pouco.",
		CouldNotSendConnection: "Não foi possível enviar. Verifique a ligação e tente novamente.",
		HandoffIntro:           "Enviamos esta conversa à equipa de {name} com a sua nota. Entretanto, pode continuar a perguntar à IA.", Email: "Email", Name: "Nome",
		Optional: "(opcional)", AnythingToAdd: "Algo a acrescentar?", SentHas: "Enviado. {name} recebeu a sua mensagem.",
		SentReply: "Enviado. {name} responderá por email.", Reference: "Referência {ref}. Pode continuar a perguntar aqui.",
		SkipToChat: "Saltar para a conversa", NeedsJS: "O chat com IA precisa de JavaScript.", SearchArticles: "Pesquisar os artigos", OrGoBackTo: "ou voltar a",
		WidgetLabel: "Chat com IA: perguntar a {name}"},
	"pt-br": {AITag: "IA", AITagTitle: "As respostas são escritas por IA", BackToHelpCenter: "Voltar para a central de ajuda", AskName: "Perguntar para {name}",
		TalkToPerson: "Falar com uma pessoa", MessageAgain: "Escrever de novo para a equipe", NewChat: "Nova conversa",
		AskAboutArticle: "Perguntar sobre este artigo", ThisArticle: "Este artigo", OpenFullChat: "Abrir na conversa completa",
		OpenFullChatTab: "Abrir a conversa completa em uma nova aba", CloseChat: "Fechar conversa", AskingAbout: "Perguntando sobre:",
		StopAskingAbout: "Parar de perguntar sobre “{title}”", NoLongerAbout: "As perguntas não são mais sobre este artigo.", AskQuestion: "Faça uma pergunta",
		AskQuestionAbout: "Faça uma pergunta sobre {name}", SendQuestion: "Enviar pergunta", YourQuestion: "Sua pergunta",
		NoteShort: "As respostas da IA podem estar erradas. Salva neste navegador por 30 dias.",
		NoteLong:  "A IA responde com os artigos de ajuda de {name} e pode errar. Esta conversa fica salva neste navegador por 30 dias.",
		Shorten:   "Encurte sua pergunta para {limit} caracteres.", Suggested: "Perguntas sugeridas", HowCanWeHelp: "Como podemos ajudar?",
		Assistant: "Assistente de IA · responde com os artigos de ajuda de {name}", QuestionAndAnswer: "Pergunta e resposta", YouAsked: "Você perguntou:",
		SourceOf: "Fonte {n}: {title}", Source: "Fonte", Sources: "Fontes", Helpful: "Útil", NotHelpful: "Não ajudou", Thanks: "Obrigado pelo feedback",
		CopyAnswer: "Copiar resposta", Copied: "Copiado", Paused: "As respostas da IA estão pausadas no momento.",
		PausedMatches: "Estes artigos correspondem à sua pergunta:", StillRead: "Você ainda pode ler os artigos de ajuda.",
		NotInArticles: "Não encontramos isso nos artigos de ajuda.", ContactsMissing: "Alguns dados de contato não estavam nos artigos.",
		TeamContacts: "A equipe pode passar os dados de contato dela.", WentWrong: "Algo deu errado.", DidNotFinish: "A resposta não terminou.",
		TryAgain: "Tentar de novo", OnItsWay: "Resposta a caminho", TooMany: "Perguntas demais, espere um minuto.",
		NotSent: "Sua pergunta não foi enviada. Pergunte de novo quando o cronômetro terminar.", CouldNotSend: "Não foi possível enviar.",
		CheckConnection: "Verifique sua conexão e tente de novo. Sua pergunta continua na caixa.", ReadyOne: "Resposta pronta, 1 fonte.",
		ReadyN: "Resposta pronta, {n} fontes.", PausedShort: "As respostas da IA estão pausadas.",
		ReadyNotFound: "Resposta pronta. Não encontramos isso nos artigos de ajuda.", Reconnecting: "Reconectando…",
		Reconnected: "Reconectado. Resposta pronta.", AskAgainIn: "Você pode perguntar de novo em {time}", AskAgain: "Você já pode perguntar de novo.",
		Unavailable: "O chat com IA não está disponível agora.", GoToHelpCenter: "Ir para a central de ajuda", ChatLong: "Esta conversa está longa.",
		StartNewToKeep: "Comece uma nova conversa para continuar perguntando.", ContinueTitle: "Continuar sua conversa anterior?", QuestionsOne: "1 pergunta",
		QuestionsN: "{n} perguntas", ActiveOne: "última atividade há 1 dia", ActiveN: "última atividade há {n} dias",
		Resume: "{questions} · {active}. Ela fica oculta até você continuar.", Continue: "Continuar", PreviousOpened: "Conversa anterior aberta.",
		ChooseFirst: "Escolha primeiro Continuar ou Nova conversa", NewStarted: "Nova conversa iniciada.", StartNewTitle: "Começar uma nova conversa?",
		ClearedHere: "Esta será apagada deste navegador.", Cancel: "Cancelar", StartNew: "Começar nova conversa",
		ContinuesInTab: "Esta conversa continua na nova aba.", SendTo: "Enviar para {name}", Sending: "Enviando…",
		EmailInvalid: "Digite um e-mail como nome@example.com.", TooManyRequests: "Solicitações demais, tente de novo em um minuto.",
		CouldNotSendLater:      "Não foi possível enviar. Tente de novo em instantes.",
		CouldNotSendConnection: "Não foi possível enviar. Verifique sua conexão e tente de novo.",
		HandoffIntro:           "Enviamos esta conversa para a equipe de {name} com a sua nota. Enquanto isso, você pode continuar perguntando à IA.", Email: "E-mail",
		Name: "Nome", Optional: "(opcional)", AnythingToAdd: "Algo a acrescentar?", SentHas: "Enviado. {name} recebeu sua mensagem.",
		SentReply: "Enviado. {name} vai responder por e-mail.", Reference: "Referência {ref}. Você pode continuar perguntando aqui.",
		SkipToChat: "Pular para a conversa", NeedsJS: "O chat com IA precisa de JavaScript.", SearchArticles: "Pesquisar os artigos",
		OrGoBackTo: "ou voltar para", WidgetLabel: "Chat com IA: perguntar para {name}"},
	"nl": {AITag: "AI", AITagTitle: "Antwoorden worden door AI geschreven", BackToHelpCenter: "Terug naar het helpcentrum", AskName: "Vraag het {name}",
		TalkToPerson: "Met een persoon praten", MessageAgain: "Het team opnieuw berichten", NewChat: "Nieuwe chat", AskAboutArticle: "Vraag over dit artikel",
		ThisArticle: "Dit artikel", OpenFullChat: "Openen in de volledige chat", OpenFullChatTab: "Volledige chat openen in een nieuw tabblad",
		CloseChat: "Chat sluiten", AskingAbout: "Vraag over:", StopAskingAbout: "Niet meer vragen over ‘{title}’",
		NoLongerAbout: "Vragen gaan niet meer over dit artikel.", AskQuestion: "Stel een vraag", AskQuestionAbout: "Stel een vraag over {name}",
		SendQuestion: "Vraag versturen", YourQuestion: "Je vraag", NoteShort: "AI-antwoorden kunnen fout zijn. 30 dagen bewaard in deze browser.",
		NoteLong: "AI antwoordt vanuit de helpartikelen van {name} en kan zich vergissen. Deze chat wordt 30 dagen in deze browser bewaard.",
		Shorten:  "Maak je vraag korter dan {limit} tekens.", Suggested: "Voorgestelde vragen", HowCanWeHelp: "Hoe kunnen we helpen?",
		Assistant: "AI-assistent · antwoordt vanuit de helpartikelen van {name}", QuestionAndAnswer: "Vraag en antwoord", YouAsked: "Je vroeg:",
		SourceOf: "Bron {n}: {title}", Source: "Bron", Sources: "Bronnen", Helpful: "Nuttig", NotHelpful: "Niet nuttig", Thanks: "Bedankt voor je feedback",
		CopyAnswer: "Antwoord kopiëren", Copied: "Gekopieerd", Paused: "AI-antwoorden zijn voorlopig gepauzeerd.",
		PausedMatches: "Deze artikelen passen bij je vraag:", StillRead: "Je kunt de helpartikelen nog steeds lezen.",
		NotInArticles: "Niet gevonden in de helpartikelen.", ContactsMissing: "Sommige contactgegevens stonden niet in de artikelen.",
		TeamContacts: "Het team kan je zijn contactgegevens geven.", WentWrong: "Er ging iets mis.", DidNotFinish: "Het antwoord is niet afgemaakt.",
		TryAgain: "Opnieuw proberen", OnItsWay: "Antwoord komt eraan", TooMany: "Te veel vragen, wacht een minuut.",
		NotSent: "Je vraag is niet verstuurd. Stel hem opnieuw als de timer afloopt.", CouldNotSend: "Versturen mislukt.",
		CheckConnection: "Controleer je verbinding en probeer het opnieuw. Je vraag staat nog in het vak.", ReadyOne: "Antwoord klaar, 1 bron.",
		ReadyN: "Antwoord klaar, {n} bronnen.", PausedShort: "AI-antwoorden zijn gepauzeerd.",
		ReadyNotFound: "Antwoord klaar. Niet gevonden in de helpartikelen.", Reconnecting: "Opnieuw verbinden…", Reconnected: "Weer verbonden. Antwoord klaar.",
		AskAgainIn: "Je kunt opnieuw vragen over {time}", AskAgain: "Je kunt opnieuw vragen.", Unavailable: "De AI-chat is nu niet beschikbaar.",
		GoToHelpCenter: "Naar het helpcentrum", ChatLong: "Deze chat is lang.", StartNewToKeep: "Start een nieuwe chat om verder te vragen.",
		ContinueTitle: "Je vorige chat voortzetten?", QuestionsOne: "1 vraag", QuestionsN: "{n} vragen", ActiveOne: "laatst actief 1 dag geleden",
		ActiveN: "laatst actief {n} dagen geleden", Resume: "{questions} · {active}. Hij blijft verborgen tot je verdergaat.", Continue: "Verder",
		PreviousOpened: "Vorige chat geopend.", ChooseFirst: "Kies eerst Verder of Nieuwe chat", NewStarted: "Nieuwe chat gestart.",
		StartNewTitle: "Een nieuwe chat starten?", ClearedHere: "Deze wordt uit deze browser gewist.", Cancel: "Annuleren", StartNew: "Nieuwe chat starten",
		ContinuesInTab: "Deze chat gaat verder in het nieuwe tabblad.", SendTo: "Versturen naar {name}", Sending: "Versturen…",
		EmailInvalid: "Vul een e-mailadres in zoals naam@example.com.", TooManyRequests: "Te veel verzoeken, probeer het over een minuut opnieuw.",
		CouldNotSendLater:      "Versturen mislukt. Probeer het zo opnieuw.",
		CouldNotSendConnection: "Versturen mislukt. Controleer je verbinding en probeer het opnieuw.",
		HandoffIntro:           "We sturen deze chat met je notitie naar het team van {name}. Je kunt de AI ondertussen blijven vragen.", Email: "E-mail", Name: "Naam",
		Optional: "(optioneel)", AnythingToAdd: "Nog iets toe te voegen?", SentHas: "Verstuurd. {name} heeft je bericht.",
		SentReply: "Verstuurd. {name} antwoordt per e-mail.", Reference: "Referentie {ref}. Je kunt hier verder vragen.", SkipToChat: "Naar de chat",
		NeedsJS: "De AI-chat heeft JavaScript nodig.", SearchArticles: "Zoek in de artikelen", OrGoBackTo: "of ga terug naar",
		WidgetLabel: "AI-chat: vraag het {name}"},
	"ja": {AITag: "AI", AITagTitle: "回答は AI が作成しています", BackToHelpCenter: "ヘルプセンターに戻る", AskName: "{name} に質問", TalkToPerson: "担当者と話す", MessageAgain: "もう一度チームに連絡する",
		NewChat: "新しいチャット", AskAboutArticle: "この記事について質問", ThisArticle: "この記事", OpenFullChat: "フルチャットで開く", OpenFullChatTab: "フルチャットを新しいタブで開く",
		CloseChat: "チャットを閉じる", AskingAbout: "質問対象:", StopAskingAbout: "「{title}」についての質問をやめる", NoLongerAbout: "質問はこの記事についてではなくなりました。", AskQuestion: "質問を入力",
		AskQuestionAbout: "{name} について質問を入力", SendQuestion: "質問を送信", YourQuestion: "質問", NoteShort: "AI の回答は誤っている場合があります。このブラウザに 30 日間保存されます。",
		NoteLong: "AI は {name} のヘルプ記事をもとに回答しますが、誤っている場合があります。このチャットはこのブラウザに 30 日間保存されます。", Shorten: "質問を {limit} 文字以内にしてください。", Suggested: "質問の例",
		HowCanWeHelp: "どのようなご用件ですか？", Assistant: "AI アシスタント · {name} のヘルプ記事をもとに回答", QuestionAndAnswer: "質問と回答", YouAsked: "あなたの質問:", SourceOf: "出典 {n}: {title}",
		Source: "出典", Sources: "出典", Helpful: "役に立った", NotHelpful: "役に立たなかった", Thanks: "フィードバックありがとうございます", CopyAnswer: "回答をコピー", Copied: "コピーしました",
		Paused: "AI の回答は現在停止しています。", PausedMatches: "質問に一致する記事:", StillRead: "ヘルプ記事は引き続きお読みいただけます。", NotInArticles: "ヘルプ記事に見つかりませんでした。",
		ContactsMissing: "一部の連絡先は記事に載っていませんでした。", TeamContacts: "連絡先はチームからお伝えできます。", WentWrong: "問題が発生しました。", DidNotFinish: "回答が完了しませんでした。", TryAgain: "再試行",
		OnItsWay: "回答を作成中", TooMany: "質問が多すぎます。1 分お待ちください。", NotSent: "質問は送信されていません。タイマーが終わったらもう一度質問してください。", CouldNotSend: "送信できませんでした。",
		CheckConnection: "接続を確認して再試行してください。質問は入力欄に残っています。", ReadyOne: "回答ができました。出典 1 件。", ReadyN: "回答ができました。出典 {n} 件。", PausedShort: "AI の回答は停止しています。",
		ReadyNotFound: "回答ができました。ヘルプ記事に見つかりませんでした。", Reconnecting: "再接続中…", Reconnected: "再接続しました。回答ができました。", AskAgainIn: "{time} 後にもう一度質問できます",
		AskAgain: "もう一度質問できます。", Unavailable: "AI チャットは現在ご利用いただけません。", GoToHelpCenter: "ヘルプセンターへ", ChatLong: "このチャットは長くなりました。",
		StartNewToKeep: "質問を続けるには新しいチャットを始めてください。", ContinueTitle: "前回のチャットを続けますか？", QuestionsOne: "質問 1 件", QuestionsN: "質問 {n} 件", ActiveOne: "最終利用 1 日前",
		ActiveN: "最終利用 {n} 日前", Resume: "{questions} · {active}。続けるまで表示されません。", Continue: "続ける", PreviousOpened: "前回のチャットを開きました。",
		ChooseFirst: "先に「続ける」か「新しいチャット」を選んでください", NewStarted: "新しいチャットを始めました。", StartNewTitle: "新しいチャットを始めますか？", ClearedHere: "このチャットはこのブラウザから消去されます。",
		Cancel: "キャンセル", StartNew: "新しいチャットを始める", ContinuesInTab: "このチャットは新しいタブで続きます。", SendTo: "{name} に送信", Sending: "送信中…",
		EmailInvalid: "name@example.com のようなメールアドレスを入力してください。", TooManyRequests: "リクエストが多すぎます。1 分後に再試行してください。",
		CouldNotSendLater: "送信できませんでした。少し待ってから再試行してください。", CouldNotSendConnection: "送信できませんでした。接続を確認して再試行してください。",
		HandoffIntro: "このチャットをメモと一緒に {name} のチームへ送ります。その間も AI に質問を続けられます。", Email: "メールアドレス", Name: "お名前", Optional: "(任意)", AnythingToAdd: "補足はありますか？",
		SentHas: "送信しました。{name} にメッセージが届きました。", SentReply: "送信しました。{name} からメールで返信があります。", Reference: "受付番号 {ref}。ここで質問を続けられます。", SkipToChat: "チャットへ移動",
		NeedsJS: "AI チャットには JavaScript が必要です。", SearchArticles: "記事を検索", OrGoBackTo: "または戻る:", WidgetLabel: "AI チャット: {name} に質問"},
	"zh-cn": {AITag: "AI", AITagTitle: "回答由 AI 撰写", BackToHelpCenter: "返回帮助中心", AskName: "向 {name} 提问", TalkToPerson: "联系人工客服", MessageAgain: "再次联系团队", NewChat: "新对话",
		AskAboutArticle: "就本文提问", ThisArticle: "本文", OpenFullChat: "在完整对话中打开", OpenFullChatTab: "在新标签页中打开完整对话", CloseChat: "关闭对话", AskingAbout: "正在询问：",
		StopAskingAbout: "不再询问“{title}”", NoLongerAbout: "问题不再针对本文。", AskQuestion: "输入问题", AskQuestionAbout: "输入关于 {name} 的问题", SendQuestion: "发送问题",
		YourQuestion: "你的问题", NoteShort: "AI 回答可能有误。在此浏览器中保存 30 天。", NoteLong: "AI 根据 {name} 的帮助文章回答，可能有误。此对话在此浏览器中保存 30 天。", Shorten: "请将问题缩短到 {limit} 个字符以内。",
		Suggested: "推荐问题", HowCanWeHelp: "有什么可以帮你？", Assistant: "AI 助手 · 根据 {name} 的帮助文章回答", QuestionAndAnswer: "问题与回答", YouAsked: "你的问题：",
		SourceOf: "来源 {n}：{title}", Source: "来源", Sources: "来源", Helpful: "有帮助", NotHelpful: "没有帮助", Thanks: "感谢你的反馈", CopyAnswer: "复制回答", Copied: "已复制",
		Paused: "AI 回答暂时停止。", PausedMatches: "以下文章与你的问题相关：", StillRead: "你仍然可以阅读帮助文章。", NotInArticles: "帮助文章中没有找到相关内容。", ContactsMissing: "部分联系方式不在文章中。",
		TeamContacts: "团队可以告诉你他们的联系方式。", WentWrong: "出了点问题。", DidNotFinish: "回答未完成。", TryAgain: "重试", OnItsWay: "正在生成回答", TooMany: "提问太频繁，请等待一分钟。",
		NotSent: "你的问题没有发送。计时结束后请再次提问。", CouldNotSend: "无法发送。", CheckConnection: "请检查网络连接后重试。你的问题仍在输入框中。", ReadyOne: "回答已完成，1 个来源。", ReadyN: "回答已完成，{n} 个来源。",
		PausedShort: "AI 回答已暂停。", ReadyNotFound: "回答已完成。帮助文章中没有找到相关内容。", Reconnecting: "正在重新连接…", Reconnected: "已重新连接。回答已完成。", AskAgainIn: "{time} 后可以再次提问",
		AskAgain: "现在可以再次提问。", Unavailable: "AI 对话暂时不可用。", GoToHelpCenter: "前往帮助中心", ChatLong: "此对话已经很长。", StartNewToKeep: "开始新对话以继续提问。",
		ContinueTitle: "继续上次的对话？", QuestionsOne: "1 个问题", QuestionsN: "{n} 个问题", ActiveOne: "最后活跃于 1 天前", ActiveN: "最后活跃于 {n} 天前",
		Resume: "{questions} · {active}。继续之前不会显示。", Continue: "继续", PreviousOpened: "已打开上次的对话。", ChooseFirst: "请先选择“继续”或“新对话”", NewStarted: "已开始新对话。",
		StartNewTitle: "开始新对话？", ClearedHere: "当前对话将从此浏览器中清除。", Cancel: "取消", StartNew: "开始新对话", ContinuesInTab: "此对话将在新标签页中继续。", SendTo: "发送给 {name}",
		Sending: "正在发送…", EmailInvalid: "请输入邮箱地址，例如 name@example.com。", TooManyRequests: "请求太频繁，请一分钟后重试。", CouldNotSendLater: "无法发送。请稍后重试。",
		CouldNotSendConnection: "无法发送。请检查网络连接后重试。", HandoffIntro: "我们会将此对话连同你的留言发送给 {name} 团队。在此期间你可以继续向 AI 提问。", Email: "邮箱", Name: "姓名", Optional: "（选填）",
		AnythingToAdd: "还有要补充的吗？", SentHas: "已发送。{name} 已收到你的消息。", SentReply: "已发送。{name} 会通过邮件回复你。", Reference: "编号 {ref}。你可以继续在这里提问。", SkipToChat: "跳到对话",
		NeedsJS: "AI 对话需要 JavaScript。", SearchArticles: "搜索文章", OrGoBackTo: "或返回", WidgetLabel: "AI 对话：向 {name} 提问"},
	"ko": {AITag: "AI", AITagTitle: "답변은 AI가 작성합니다", BackToHelpCenter: "고객센터로 돌아가기", AskName: "{name}에 질문하기", TalkToPerson: "상담원과 대화하기",
		MessageAgain: "팀에 다시 메시지 보내기", NewChat: "새 채팅", AskAboutArticle: "이 문서에 대해 질문하기", ThisArticle: "이 문서", OpenFullChat: "전체 채팅으로 열기",
		OpenFullChatTab: "새 탭에서 전체 채팅 열기", CloseChat: "채팅 닫기", AskingAbout: "질문 대상:", StopAskingAbout: "‘{title}’에 대한 질문 그만하기",
		NoLongerAbout: "이제 질문이 이 문서에 관한 것이 아닙니다.", AskQuestion: "질문을 입력하세요", AskQuestionAbout: "{name}에 대해 질문을 입력하세요", SendQuestion: "질문 보내기",
		YourQuestion: "질문", NoteShort: "AI 답변은 틀릴 수 있습니다. 이 브라우저에 30일 동안 보관됩니다.",
		NoteLong: "AI는 {name}의 도움말 문서를 바탕으로 답변하며 틀릴 수 있습니다. 이 채팅은 이 브라우저에 30일 동안 보관됩니다.", Shorten: "질문을 {limit}자 이내로 줄여 주세요.", Suggested: "추천 질문",
		HowCanWeHelp: "무엇을 도와드릴까요?", Assistant: "AI 어시스턴트 · {name}의 도움말 문서를 바탕으로 답변", QuestionAndAnswer: "질문과 답변", YouAsked: "내 질문:",
		SourceOf: "출처 {n}: {title}", Source: "출처", Sources: "출처", Helpful: "도움이 됨", NotHelpful: "도움이 안 됨", Thanks: "의견을 주셔서 감사합니다", CopyAnswer: "답변 복사",
		Copied: "복사됨", Paused: "AI 답변이 일시 중지되었습니다.", PausedMatches: "질문과 관련된 문서:", StillRead: "도움말 문서는 계속 읽을 수 있습니다.", NotInArticles: "도움말 문서에서 찾지 못했습니다.",
		ContactsMissing: "일부 연락처 정보는 문서에 없었습니다.", TeamContacts: "연락처는 팀에서 알려 드릴 수 있습니다.", WentWrong: "문제가 발생했습니다.", DidNotFinish: "답변이 완료되지 않았습니다.",
		TryAgain: "다시 시도", OnItsWay: "답변 작성 중", TooMany: "질문이 너무 많습니다. 1분 기다려 주세요.", NotSent: "질문이 전송되지 않았습니다. 타이머가 끝나면 다시 질문해 주세요.", CouldNotSend: "보내지 못했습니다.",
		CheckConnection: "연결을 확인하고 다시 시도해 주세요. 질문은 입력란에 그대로 있습니다.", ReadyOne: "답변 완료, 출처 1개.", ReadyN: "답변 완료, 출처 {n}개.", PausedShort: "AI 답변이 일시 중지되었습니다.",
		ReadyNotFound: "답변 완료. 도움말 문서에서 찾지 못했습니다.", Reconnecting: "다시 연결 중…", Reconnected: "다시 연결되었습니다. 답변 완료.", AskAgainIn: "{time} 후에 다시 질문할 수 있습니다",
		AskAgain: "다시 질문할 수 있습니다.", Unavailable: "지금은 AI 채팅을 사용할 수 없습니다.", GoToHelpCenter: "고객센터로 이동", ChatLong: "채팅이 길어졌습니다.",
		StartNewToKeep: "계속 질문하려면 새 채팅을 시작하세요.", ContinueTitle: "이전 채팅을 계속할까요?", QuestionsOne: "질문 1개", QuestionsN: "질문 {n}개", ActiveOne: "1일 전 마지막 활동",
		ActiveN: "{n}일 전 마지막 활동", Resume: "{questions} · {active}. 계속하기 전까지 표시되지 않습니다.", Continue: "계속하기", PreviousOpened: "이전 채팅을 열었습니다.",
		ChooseFirst: "먼저 계속하기 또는 새 채팅을 선택하세요", NewStarted: "새 채팅을 시작했습니다.", StartNewTitle: "새 채팅을 시작할까요?", ClearedHere: "현재 채팅은 이 브라우저에서 삭제됩니다.", Cancel: "취소",
		StartNew: "새 채팅 시작", ContinuesInTab: "이 채팅은 새 탭에서 계속됩니다.", SendTo: "{name}에 보내기", Sending: "보내는 중…", EmailInvalid: "name@example.com 형식의 이메일 주소를 입력하세요.",
		TooManyRequests: "요청이 너무 많습니다. 1분 후 다시 시도하세요.", CouldNotSendLater: "보내지 못했습니다. 잠시 후 다시 시도하세요.", CouldNotSendConnection: "보내지 못했습니다. 연결을 확인하고 다시 시도하세요.",
		HandoffIntro: "이 채팅을 메모와 함께 {name} 팀에 보냅니다. 그동안 AI에게 계속 질문할 수 있습니다.", Email: "이메일", Name: "이름", Optional: "(선택)", AnythingToAdd: "덧붙일 내용이 있나요?",
		SentHas: "보냈습니다. {name}에서 메시지를 받았습니다.", SentReply: "보냈습니다. {name}에서 이메일로 답장합니다.", Reference: "접수 번호 {ref}. 여기서 계속 질문할 수 있습니다.", SkipToChat: "채팅으로 건너뛰기",
		NeedsJS: "AI 채팅을 사용하려면 JavaScript가 필요합니다.", SearchArticles: "문서 검색", OrGoBackTo: "또는 돌아가기:", WidgetLabel: "AI 채팅: {name}에 질문하기"},
	"vi": {AITag: "AI", AITagTitle: "Câu trả lời do AI viết", BackToHelpCenter: "Về trung tâm trợ giúp", AskName: "Hỏi {name}",
		TalkToPerson: "Nói chuyện với nhân viên", MessageAgain: "Nhắn lại cho đội ngũ", NewChat: "Cuộc trò chuyện mới", AskAboutArticle: "Hỏi về bài viết này",
		ThisArticle: "Bài viết này", OpenFullChat: "Mở trong khung trò chuyện đầy đủ", OpenFullChatTab: "Mở khung trò chuyện đầy đủ trong thẻ mới",
		CloseChat: "Đóng trò chuyện", AskingAbout: "Đang hỏi về:", StopAskingAbout: "Thôi hỏi về “{title}”",
		NoLongerAbout: "Câu hỏi không còn gắn với bài viết này.", AskQuestion: "Đặt câu hỏi", AskQuestionAbout: "Đặt câu hỏi về {name}",
		SendQuestion: "Gửi câu hỏi", YourQuestion: "Câu hỏi của bạn", NoteShort: "Câu trả lời của AI có thể sai. Lưu trên trình duyệt này trong 30 ngày.",
		NoteLong: "AI trả lời dựa trên bài viết trợ giúp của {name} và có thể sai. Cuộc trò chuyện này được lưu trên trình duyệt này trong 30 ngày.",
		Shorten:  "Rút gọn câu hỏi còn {limit} ký tự.", Suggested: "Câu hỏi gợi ý", HowCanWeHelp: "Chúng tôi có thể giúp gì?",
		Assistant: "Trợ lý AI · trả lời dựa trên bài viết trợ giúp của {name}", QuestionAndAnswer: "Câu hỏi và câu trả lời", YouAsked: "Bạn đã hỏi:",
		SourceOf: "Nguồn {n}: {title}", Source: "Nguồn", Sources: "Nguồn", Helpful: "Hữu ích", NotHelpful: "Không hữu ích", Thanks: "Cảm ơn bạn đã góp ý",
		CopyAnswer: "Sao chép câu trả lời", Copied: "Đã sao chép", Paused: "Câu trả lời AI đang tạm dừng.",
		PausedMatches: "Các bài viết này khớp với câu hỏi của bạn:", StillRead: "Bạn vẫn có thể đọc các bài viết trợ giúp.",
		NotInArticles: "Không tìm thấy nội dung này trong bài viết trợ giúp.", ContactsMissing: "Một số thông tin liên hệ không có trong bài viết.",
		TeamContacts: "Đội ngũ có thể cho bạn thông tin liên hệ.", WentWrong: "Đã xảy ra lỗi.", DidNotFinish: "Câu trả lời chưa hoàn tất.", TryAgain: "Thử lại",
		OnItsWay: "Đang soạn câu trả lời", TooMany: "Bạn hỏi quá nhiều, hãy đợi một phút.", NotSent: "Câu hỏi chưa được gửi. Hãy hỏi lại khi hết thời gian chờ.",
		CouldNotSend: "Không gửi được.", CheckConnection: "Kiểm tra kết nối rồi thử lại. Câu hỏi của bạn vẫn còn trong ô nhập.",
		ReadyOne: "Đã có câu trả lời, 1 nguồn.", ReadyN: "Đã có câu trả lời, {n} nguồn.", PausedShort: "Câu trả lời AI đang tạm dừng.",
		ReadyNotFound: "Đã có câu trả lời. Không tìm thấy nội dung này trong bài viết trợ giúp.", Reconnecting: "Đang kết nối lại…",
		Reconnected: "Đã kết nối lại. Đã có câu trả lời.", AskAgainIn: "Bạn có thể hỏi lại sau {time}", AskAgain: "Bạn có thể hỏi lại.",
		Unavailable: "Trò chuyện AI hiện không khả dụng.", GoToHelpCenter: "Đến trung tâm trợ giúp", ChatLong: "Cuộc trò chuyện này đã dài.",
		StartNewToKeep: "Bắt đầu cuộc trò chuyện mới để hỏi tiếp.", ContinueTitle: "Tiếp tục cuộc trò chuyện trước?", QuestionsOne: "1 câu hỏi",
		QuestionsN: "{n} câu hỏi", ActiveOne: "hoạt động lần cuối 1 ngày trước", ActiveN: "hoạt động lần cuối {n} ngày trước",
		Resume: "{questions} · {active}. Nội dung được ẩn cho tới khi bạn tiếp tục.", Continue: "Tiếp tục", PreviousOpened: "Đã mở cuộc trò chuyện trước.",
		ChooseFirst: "Chọn Tiếp tục hoặc Cuộc trò chuyện mới trước", NewStarted: "Đã bắt đầu cuộc trò chuyện mới.",
		StartNewTitle: "Bắt đầu cuộc trò chuyện mới?", ClearedHere: "Cuộc trò chuyện này sẽ bị xoá khỏi trình duyệt.", Cancel: "Huỷ",
		StartNew: "Bắt đầu cuộc trò chuyện mới", ContinuesInTab: "Cuộc trò chuyện này tiếp tục ở thẻ mới.", SendTo: "Gửi cho {name}", Sending: "Đang gửi…",
		EmailInvalid: "Nhập địa chỉ email dạng ten@example.com.", TooManyRequests: "Quá nhiều yêu cầu, hãy thử lại sau một phút.",
		CouldNotSendLater: "Không gửi được. Hãy thử lại sau giây lát.", CouldNotSendConnection: "Không gửi được. Kiểm tra kết nối rồi thử lại.",
		HandoffIntro: "Chúng tôi gửi cuộc trò chuyện này kèm ghi chú của bạn cho đội ngũ {name}. Trong lúc chờ, bạn vẫn có thể hỏi AI.", Email: "Email",
		Name: "Tên", Optional: "(không bắt buộc)", AnythingToAdd: "Bạn muốn bổ sung gì không?", SentHas: "Đã gửi. {name} đã nhận tin nhắn của bạn.",
		SentReply: "Đã gửi. {name} sẽ trả lời qua email.", Reference: "Mã tham chiếu {ref}. Bạn có thể tiếp tục hỏi ở đây.",
		SkipToChat: "Chuyển đến khung trò chuyện", NeedsJS: "Trò chuyện AI cần JavaScript.", SearchArticles: "Tìm trong bài viết", OrGoBackTo: "hoặc quay về",
		WidgetLabel: "Trò chuyện AI: hỏi {name}"},
	"pl": {AITag: "AI", AITagTitle: "Odpowiedzi pisze AI", BackToHelpCenter: "Wróć do centrum pomocy", AskName: "Zapytaj {name}",
		TalkToPerson: "Porozmawiaj z człowiekiem", MessageAgain: "Napisz ponownie do zespołu", NewChat: "Nowy czat", AskAboutArticle: "Zapytaj o ten artykuł",
		ThisArticle: "Ten artykuł", OpenFullChat: "Otwórz w pełnym czacie", OpenFullChatTab: "Otwórz pełny czat w nowej karcie", CloseChat: "Zamknij czat",
		AskingAbout: "Pytanie o:", StopAskingAbout: "Przestań pytać o „{title}”", NoLongerAbout: "Pytania nie dotyczą już tego artykułu.",
		AskQuestion: "Zadaj pytanie", AskQuestionAbout: "Zadaj pytanie o {name}", SendQuestion: "Wyślij pytanie", YourQuestion: "Twoje pytanie",
		NoteShort: "Odpowiedzi AI mogą być błędne. Przechowywany w tej przeglądarce przez 30 dni.",
		NoteLong:  "AI odpowiada na podstawie artykułów pomocy {name} i może się mylić. Ten czat jest przechowywany w tej przeglądarce przez 30 dni.",
		Shorten:   "Skróć pytanie do {limit} znaków.", Suggested: "Proponowane pytania", HowCanWeHelp: "Jak możemy pomóc?",
		Assistant: "Asystent AI · odpowiada na podstawie artykułów pomocy {name}", QuestionAndAnswer: "Pytanie i odpowiedź", YouAsked: "Twoje pytanie:",
		SourceOf: "Źródło {n}: {title}", Source: "Źródło", Sources: "Źródła", Helpful: "Pomocne", NotHelpful: "Niepomocne", Thanks: "Dziękujemy za opinię",
		CopyAnswer: "Kopiuj odpowiedź", Copied: "Skopiowano", Paused: "Odpowiedzi AI są na razie wstrzymane.",
		PausedMatches: "Te artykuły pasują do Twojego pytania:", StillRead: "Nadal możesz czytać artykuły pomocy.",
		NotInArticles: "Nie znaleziono tego w artykułach pomocy.", ContactsMissing: "Niektórych danych kontaktowych nie było w artykułach.",
		TeamContacts: "Zespół może podać Ci swoje dane kontaktowe.", WentWrong: "Coś poszło nie tak.", DidNotFinish: "Odpowiedź nie została dokończona.",
		TryAgain: "Spróbuj ponownie", OnItsWay: "Odpowiedź w drodze", TooMany: "Za dużo pytań, poczekaj minutę.",
		NotSent: "Twoje pytanie nie zostało wysłane. Zadaj je ponownie, gdy minie czas.", CouldNotSend: "Nie udało się wysłać.",
		CheckConnection: "Sprawdź połączenie i spróbuj ponownie. Twoje pytanie nadal jest w polu.", ReadyOne: "Odpowiedź gotowa, źródła: 1.",
		ReadyN: "Odpowiedź gotowa, źródła: {n}.", PausedShort: "Odpowiedzi AI są wstrzymane.",
		ReadyNotFound: "Odpowiedź gotowa. Nie znaleziono tego w artykułach pomocy.", Reconnecting: "Ponowne łączenie…",
		Reconnected: "Połączono ponownie. Odpowiedź gotowa.", AskAgainIn: "Możesz zapytać ponownie za {time}", AskAgain: "Możesz zapytać ponownie.",
		Unavailable: "Czat AI jest teraz niedostępny.", GoToHelpCenter: "Przejdź do centrum pomocy", ChatLong: "Ten czat jest długi.",
		StartNewToKeep: "Zacznij nowy czat, aby dalej pytać.", ContinueTitle: "Kontynuować poprzedni czat?", QuestionsOne: "Pytania: 1",
		QuestionsN: "Pytania: {n}", ActiveOne: "ostatnia aktywność 1 dzień temu", ActiveN: "ostatnia aktywność {n} dn. temu",
		Resume: "{questions} · {active}. Pozostaje ukryty, dopóki nie wybierzesz Kontynuuj.", Continue: "Kontynuuj", PreviousOpened: "Otwarto poprzedni czat.",
		ChooseFirst: "Najpierw wybierz Kontynuuj lub Nowy czat", NewStarted: "Rozpoczęto nowy czat.", StartNewTitle: "Rozpocząć nowy czat?",
		ClearedHere: "Ten czat zostanie usunięty z tej przeglądarki.", Cancel: "Anuluj", StartNew: "Rozpocznij nowy czat",
		ContinuesInTab: "Ten czat jest kontynuowany w nowej karcie.", SendTo: "Wyślij do {name}", Sending: "Wysyłanie…",
		EmailInvalid: "Wpisz adres e-mail, np. imie@example.com.", TooManyRequests: "Za dużo żądań, spróbuj ponownie za minutę.",
		CouldNotSendLater:      "Nie udało się wysłać. Spróbuj ponownie za chwilę.",
		CouldNotSendConnection: "Nie udało się wysłać. Sprawdź połączenie i spróbuj ponownie.",
		HandoffIntro:           "Wyślemy ten czat z Twoją notatką do zespołu {name}. W międzyczasie możesz dalej pytać AI.", Email: "E-mail", Name: "Imię i nazwisko",
		Optional: "(opcjonalnie)", AnythingToAdd: "Chcesz coś dodać?", SentHas: "Wysłano. {name} ma Twoją wiadomość.",
		SentReply: "Wysłano. {name} odpowie e-mailem.", Reference: "Numer zgłoszenia {ref}. Możesz dalej pytać tutaj.", SkipToChat: "Przejdź do czatu",
		NeedsJS: "Czat AI wymaga JavaScriptu.", SearchArticles: "Szukaj w artykułach", OrGoBackTo: "lub wróć do", WidgetLabel: "Czat AI: zapytaj {name}"},
	"sv": {AITag: "AI", AITagTitle: "Svaren skrivs av AI", BackToHelpCenter: "Tillbaka till hjälpcentret", AskName: "Fråga {name}",
		TalkToPerson: "Prata med en person", MessageAgain: "Skriv till teamet igen", NewChat: "Ny chatt", AskAboutArticle: "Fråga om den här artikeln",
		ThisArticle: "Den här artikeln", OpenFullChat: "Öppna i hela chatten", OpenFullChatTab: "Öppna hela chatten i en ny flik", CloseChat: "Stäng chatten",
		AskingAbout: "Frågar om:", StopAskingAbout: "Sluta fråga om ”{title}”", NoLongerAbout: "Frågorna gäller inte längre den här artikeln.",
		AskQuestion: "Ställ en fråga", AskQuestionAbout: "Ställ en fråga om {name}", SendQuestion: "Skicka frågan", YourQuestion: "Din fråga",
		NoteShort: "AI-svar kan vara fel. Sparas i den här webbläsaren i 30 dagar.",
		NoteLong:  "AI svarar utifrån hjälpartiklarna från {name} och kan ha fel. Chatten sparas i den här webbläsaren i 30 dagar.",
		Shorten:   "Korta frågan till {limit} tecken.", Suggested: "Föreslagna frågor", HowCanWeHelp: "Hur kan vi hjälpa till?",
		Assistant: "AI-assistent · svarar utifrån hjälpartiklarna från {name}", QuestionAndAnswer: "Fråga och svar", YouAsked: "Du frågade:",
		SourceOf: "Källa {n}: {title}", Source: "Källa", Sources: "Källor", Helpful: "Hjälpsamt", NotHelpful: "Inte hjälpsamt",
		Thanks: "Tack för din återkoppling", CopyAnswer: "Kopiera svaret", Copied: "Kopierat", Paused: "AI-svar är pausade just nu.",
		PausedMatches: "De här artiklarna matchar din fråga:", StillRead: "Du kan fortfarande läsa hjälpartiklarna.",
		NotInArticles: "Hittade inte detta i hjälpartiklarna.", ContactsMissing: "Vissa kontaktuppgifter fanns inte i artiklarna.",
		TeamContacts: "Teamet kan ge dig sina kontaktuppgifter.", WentWrong: "Något gick fel.", DidNotFinish: "Svaret blev inte klart.", TryAgain: "Försök igen",
		OnItsWay: "Svaret är på väg", TooMany: "För många frågor, vänta en minut.", NotSent: "Din fråga skickades inte. Ställ den igen när timern är slut.",
		CouldNotSend: "Kunde inte skicka.", CheckConnection: "Kontrollera anslutningen och försök igen. Din fråga finns kvar i rutan.",
		ReadyOne: "Svaret är klart, 1 källa.", ReadyN: "Svaret är klart, {n} källor.", PausedShort: "AI-svar är pausade.",
		ReadyNotFound: "Svaret är klart. Hittade inte detta i hjälpartiklarna.", Reconnecting: "Ansluter igen…", Reconnected: "Ansluten igen. Svaret är klart.",
		AskAgainIn: "Du kan fråga igen om {time}", AskAgain: "Du kan fråga igen.", Unavailable: "AI-chatten är inte tillgänglig just nu.",
		GoToHelpCenter: "Gå till hjälpcentret", ChatLong: "Den här chatten är lång.", StartNewToKeep: "Starta en ny chatt för att fortsätta fråga.",
		ContinueTitle: "Fortsätta din förra chatt?", QuestionsOne: "1 fråga", QuestionsN: "{n} frågor", ActiveOne: "senast aktiv för 1 dag sedan",
		ActiveN: "senast aktiv för {n} dagar sedan", Resume: "{questions} · {active}. Den förblir dold tills du fortsätter.", Continue: "Fortsätt",
		PreviousOpened: "Förra chatten öppnad.", ChooseFirst: "Välj Fortsätt eller Ny chatt först", NewStarted: "Ny chatt startad.",
		StartNewTitle: "Starta en ny chatt?", ClearedHere: "Den här rensas från webbläsaren.", Cancel: "Avbryt", StartNew: "Starta ny chatt",
		ContinuesInTab: "Chatten fortsätter i den nya fliken.", SendTo: "Skicka till {name}", Sending: "Skickar…",
		EmailInvalid: "Ange en e-postadress som namn@example.com.", TooManyRequests: "För många förfrågningar, försök igen om en minut.",
		CouldNotSendLater: "Kunde inte skicka. Försök igen om en stund.", CouldNotSendConnection: "Kunde inte skicka. Kontrollera anslutningen och försök igen.",
		HandoffIntro: "Vi skickar chatten till teamet på {name} med din kommentar. Du kan fortsätta fråga AI under tiden.", Email: "E-post", Name: "Namn",
		Optional: "(valfritt)", AnythingToAdd: "Något att lägga till?", SentHas: "Skickat. {name} har fått ditt meddelande.",
		SentReply: "Skickat. {name} svarar via e-post.", Reference: "Referens {ref}. Du kan fortsätta fråga här.", SkipToChat: "Hoppa till chatten",
		NeedsJS: "AI-chatten kräver JavaScript.", SearchArticles: "Sök i artiklarna", OrGoBackTo: "eller gå tillbaka till",
		WidgetLabel: "AI-chatt: fråga {name}"},
	"ru": {AITag: "ИИ", AITagTitle: "Ответы пишет ИИ", BackToHelpCenter: "Назад в справочный центр", AskName: "Спросить {name}",
		TalkToPerson: "Связаться с сотрудником", MessageAgain: "Написать команде ещё раз", NewChat: "Новый чат", AskAboutArticle: "Спросить об этой статье",
		ThisArticle: "Эта статья", OpenFullChat: "Открыть в полном чате", OpenFullChatTab: "Открыть полный чат в новой вкладке", CloseChat: "Закрыть чат",
		AskingAbout: "Вопрос о статье:", StopAskingAbout: "Больше не спрашивать о «{title}»", NoLongerAbout: "Вопросы больше не относятся к этой статье.",
		AskQuestion: "Задайте вопрос", AskQuestionAbout: "Задайте вопрос о {name}", SendQuestion: "Отправить вопрос", YourQuestion: "Ваш вопрос",
		NoteShort: "Ответы ИИ могут быть неверными. Хранится в этом браузере 30 дней.",
		NoteLong:  "ИИ отвечает по справочным статьям {name} и может ошибаться. Этот чат хранится в этом браузере 30 дней.",
		Shorten:   "Сократите вопрос до {limit} символов.", Suggested: "Предлагаемые вопросы", HowCanWeHelp: "Чем мы можем помочь?",
		Assistant: "ИИ-ассистент · отвечает по справочным статьям {name}", QuestionAndAnswer: "Вопрос и ответ", YouAsked: "Ваш вопрос:",
		SourceOf: "Источник {n}: {title}", Source: "Источник", Sources: "Источники", Helpful: "Полезно", NotHelpful: "Бесполезно", Thanks: "Спасибо за отзыв",
		CopyAnswer: "Скопировать ответ", Copied: "Скопировано", Paused: "Ответы ИИ пока приостановлены.", PausedMatches: "Эти статьи подходят к вашему вопросу:",
		StillRead: "Вы по-прежнему можете читать справочные статьи.", NotInArticles: "В справочных статьях этого не нашлось.",
		ContactsMissing: "Некоторых контактных данных не было в статьях.", TeamContacts: "Команда может сообщить вам свои контакты.",
		WentWrong: "Что-то пошло не так.", DidNotFinish: "Ответ не был завершён.", TryAgain: "Повторить", OnItsWay: "Ответ готовится",
		TooMany: "Слишком много вопросов, подождите минуту.", NotSent: "Ваш вопрос не отправлен. Задайте его снова, когда закончится отсчёт.",
		CouldNotSend: "Не удалось отправить.", CheckConnection: "Проверьте подключение и попробуйте снова. Ваш вопрос остался в поле.",
		ReadyOne: "Ответ готов, источников: 1.", ReadyN: "Ответ готов, источников: {n}.", PausedShort: "Ответы ИИ приостановлены.",
		ReadyNotFound: "Ответ готов. В справочных статьях этого не нашлось.", Reconnecting: "Переподключение…",
		Reconnected: "Подключение восстановлено. Ответ готов.", AskAgainIn: "Можно спросить снова через {time}", AskAgain: "Можно спросить снова.",
		Unavailable: "ИИ-чат сейчас недоступен.", GoToHelpCenter: "Перейти в справочный центр", ChatLong: "Этот чат слишком длинный.",
		StartNewToKeep: "Начните новый чат, чтобы продолжить.", ContinueTitle: "Продолжить предыдущий чат?", QuestionsOne: "Вопросов: 1",
		QuestionsN: "Вопросов: {n}", ActiveOne: "последняя активность 1 день назад", ActiveN: "последняя активность {n} дн. назад",
		Resume: "{questions} · {active}. Он скрыт, пока вы не продолжите.", Continue: "Продолжить", PreviousOpened: "Предыдущий чат открыт.",
		ChooseFirst: "Сначала выберите «Продолжить» или «Новый чат»", NewStarted: "Новый чат начат.", StartNewTitle: "Начать новый чат?",
		ClearedHere: "Текущий чат будет удалён из этого браузера.", Cancel: "Отмена", StartNew: "Начать новый чат",
		ContinuesInTab: "Этот чат продолжается в новой вкладке.", SendTo: "Отправить команде {name}", Sending: "Отправка…",
		EmailInvalid: "Введите адрес электронной почты, например name@example.com.", TooManyRequests: "Слишком много запросов, попробуйте через минуту.",
		CouldNotSendLater:      "Не удалось отправить. Попробуйте чуть позже.",
		CouldNotSendConnection: "Не удалось отправить. Проверьте подключение и попробуйте снова.",
		HandoffIntro:           "Мы отправим этот чат с вашей заметкой команде {name}. А пока можно продолжать спрашивать ИИ.", Email: "Эл. почта", Name: "Имя",
		Optional: "(необязательно)", AnythingToAdd: "Хотите что-то добавить?", SentHas: "Отправлено. Ваше сообщение у команды {name}.",
		SentReply: "Отправлено. {name} ответит по электронной почте.", Reference: "Номер обращения {ref}. Можно продолжать спрашивать здесь.",
		SkipToChat: "Перейти к чату", NeedsJS: "Для ИИ-чата нужен JavaScript.", SearchArticles: "Искать в статьях", OrGoBackTo: "или вернуться в",
		WidgetLabel: "ИИ-чат: спросить {name}"},
}

// chatVersion names every set's words, for the pages' ETag (an article page carries its box's words).
var chatVersion = func() string {
	sum := sha256.New()
	for _, locale := range slices.Sorted(maps.Keys(chatJSON)) {
		sum.Write([]byte(locale + "\x00" + chatJSON[locale] + "\x00"))
	}
	return hex.EncodeToString(sum.Sum(nil)[:6])
}()

// chatJSON is each set as chat.js reads it, made once.
var chatJSON = func() map[string]string {
	out := make(map[string]string, len(chatTexts))
	for locale, text := range chatTexts {
		data, err := json.Marshal(text)
		if err != nil {
			panic(err)
		}
		out[locale] = string(data)
	}
	return out
}()

// ChatWords is the help chat's words for a page in locale, found as the page's own words are.
func ChatWords(locale string) ChatText { return chatTexts[textKey(locale)] }

// ChatWordsJSON is ChatWords as JSON, for chat.js.
func ChatWordsJSON(locale string) string { return chatJSON[textKey(locale)] }

// Fill puts the values in a word's placeholders: Fill("Ask {name}", "name", "Acme").
func Fill(word string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		word = strings.ReplaceAll(word, "{"+pairs[i]+"}", pairs[i+1])
	}
	return word
}
