package helpcenter

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	portalCookieName = "colliedesk_portal"
	portalBodyLimit  = 32 << 10
	portalSubjectMax = 200
	portalMessageMax = 5000
)

// portalPath is a customer portal address below the help center root.
func portalPath(sub string) bool {
	return sub == "/requests" || strings.HasPrefix(sub, "/requests/")
}

var (
	portalSeriesPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,19}$`)
	portalTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{20,80}$`)
)

// portalLinkLabel is the masthead link in the page's language. The portal's own pages are in English.
func portalLinkLabel(locale string) string {
	if label, ok := portalLinkLabels[textKey(locale)]; ok {
		return label
	}
	return portalLinkLabels["en"]
}

var portalLinkLabels = map[string]string{
	"en": "My requests", "fr": "Mes demandes", "de": "Meine Anfragen", "es": "Mis solicitudes",
	"it": "Le mie richieste", "pt": "Os meus pedidos", "pt-br": "Meus pedidos", "nl": "Mijn aanvragen",
	"ja": "マイリクエスト", "zh-cn": "我的请求", "ko": "내 요청", "vi": "Yêu cầu của tôi",
	"pl": "Moje zgłoszenia", "sv": "Mina ärenden", "ru": "Мои запросы",
}

func portalHome(base string) string {
	if base == "" {
		return "/requests"
	}
	return strings.TrimRight(base, "/") + "/requests"
}

func portalCookiePath(base string) string {
	if base == "" {
		return "/"
	}
	return base
}

func portalTicketPath(base, series string, number int64) string {
	home := portalHome(base)
	if series == "" || series == "default" {
		return home + "/ticket/" + strconv.FormatInt(number, 10)
	}
	return home + "/ticket/" + series + "/" + strconv.FormatInt(number, 10)
}

// portalPage is what the portal template shows. Mode picks the page.
type portalPage struct {
	Mode, Heading, Error, Notice, Action, Email, Subject, Message string
	SignOut, NewURL, ListURL                                      string
	Tickets                                                       []portalTicketRow
	Ticket                                                        portalTicketView
	MoreTickets, MoreMessages                                     bool
}

type portalTicketRow struct {
	URL, Ref, Subject, Status, When, WhenISO string
}

type portalTicketView struct {
	Ref, Subject, Status string
	Messages             []portalMessageView
}

type portalMessageView struct {
	By, Body, When, WhenISO string
}

// servePortal answers /requests on an open help center. When the portal is off, a read is the same
// 404 as any other missing page and a write is the same 405, so the path does not announce itself.
func (s *Site) servePortal(response http.ResponseWriter, request *http.Request, v view, sub string) {
	if !v.site.Portal {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			securityHeaders(response.Header())
			response.Header().Set("Cache-Control", "no-store")
			http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.writeSiteNotFound(response, v)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodPost {
		response.Header().Set("Allow", "GET, HEAD, POST")
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if request.Method == http.MethodPost && !portalSameOrigin(request) {
		s.writePortal(response, request, v, portalPage{Mode: "note", Heading: "Try again", Error: "Send this form from the help center page."}, http.StatusForbidden)
		return
	}
	email, err := s.portalEmail(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	home := portalHome(v.base)
	switch {
	case sub == "/requests" || sub == "/requests/":
		s.portalHome(response, request, v, email, home)
	case sub == "/requests/new":
		s.portalNew(response, request, v, email, home)
	case sub == "/requests/sent":
		if request.Method == http.MethodPost {
			http.Redirect(response, request, home, http.StatusSeeOther)
			return
		}
		s.writePortal(response, request, v, portalPage{Mode: "sent", Heading: "Check your email",
			Notice: "If that address can receive mail, a sign-in link is on its way. It works once and expires in 15 minutes.",
			Action: home}, http.StatusOK)
	case sub == "/requests/sign-out":
		s.portalSignOut(response, request, v, home)
	case strings.HasPrefix(sub, "/requests/continue/"):
		s.portalContinue(response, request, v, strings.TrimPrefix(sub, "/requests/continue/"), home)
	case strings.HasPrefix(sub, "/requests/ticket/"):
		s.portalTicket(response, request, v, email, home, strings.TrimPrefix(sub, "/requests/ticket/"))
	default:
		s.writeSiteNotFound(response, v)
	}
}

func portalSameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(requestHost(parsed.Host), requestHost(request.Host))
}

func (s *Site) portalEmail(request *http.Request, v view) (string, error) {
	cookie, err := request.Cookie(portalCookieName)
	if err != nil || cookie.Value == "" {
		return "", nil
	}
	return s.config.Store.PortalSession(request.Context(), v.site.WorkspaceID, cookie.Value)
}

func (s *Site) portalHome(response http.ResponseWriter, request *http.Request, v view, email, home string) {
	if request.Method == http.MethodPost {
		s.portalRequestLink(response, request, v, home)
		return
	}
	if email == "" {
		s.writePortal(response, request, v, portalPage{Mode: "signin", Heading: "See your requests", Action: home,
			Notice: "Enter the email address you wrote to us from. We will send a link that signs you in. It works once."}, http.StatusOK)
		return
	}
	items, more, err := s.config.Store.ListPortalTickets(request.Context(), v.site.WorkspaceID, email)
	if err != nil {
		s.writeError(response, err)
		return
	}
	page := portalPage{Mode: "list", Heading: "Your requests", Email: email, SignOut: home + "/sign-out", NewURL: home + "/new", MoreTickets: more}
	for _, item := range items {
		iso, label := portalWhen(item.Updated)
		page.Tickets = append(page.Tickets, portalTicketRow{URL: portalTicketPath(v.base, item.Series, item.Number), Ref: item.Ref,
			Subject: item.Subject, Status: portalStatus(item.Status), When: label, WhenISO: iso})
	}
	s.writePortal(response, request, v, page, http.StatusOK)
}

func (s *Site) portalRequestLink(response http.ResponseWriter, request *http.Request, v view, home string) {
	fields, ok := portalForm(response, request)
	if !ok {
		return
	}
	if strings.TrimSpace(fields.Get("website")) != "" {
		http.Redirect(response, request, home+"/sent", http.StatusSeeOther)
		return
	}
	address, message := portalEmailAddress(fields.Get("email"))
	if message != "" {
		s.writePortal(response, request, v, portalPage{Mode: "signin", Heading: "See your requests", Action: home, Error: message, Email: fields.Get("email")}, http.StatusBadRequest)
		return
	}
	if s.config.AllowPortalLink != nil {
		if allowed, wait := s.config.AllowPortalLink(request, v.site.WorkspaceID, address); !allowed {
			s.writePortalWait(response, request, v, wait)
			return
		}
	}
	if s.config.SendPortalLink == nil {
		s.writePortal(response, request, v, portalPage{Mode: "signin", Heading: "See your requests", Action: home,
			Error: "This help center cannot email a sign-in link yet. Ask the team to set up outgoing email."}, http.StatusOK)
		return
	}
	token, err := s.config.Store.CreatePortalLink(request.Context(), v.site.WorkspaceID, address)
	if err != nil {
		s.writeError(response, err)
		return
	}
	link := v.address + "/requests/continue/" + token
	sent, err := s.config.SendPortalLink(request.Context(), v.site.WorkspaceID, v.title(), address, link)
	if !sent || err != nil {
		_ = s.config.Store.ForgetPortalLink(request.Context(), v.site.WorkspaceID, token)
		text := "The email could not be sent. Try again in a minute."
		if !sent {
			text = "This help center cannot email a sign-in link yet. Ask the team to set up outgoing email."
		}
		s.writePortal(response, request, v, portalPage{Mode: "signin", Heading: "See your requests", Action: home, Error: text}, http.StatusOK)
		return
	}
	http.Redirect(response, request, home+"/sent", http.StatusSeeOther)
}

func (s *Site) portalNew(response http.ResponseWriter, request *http.Request, v view, email, home string) {
	if email == "" {
		http.Redirect(response, request, home, http.StatusSeeOther)
		return
	}
	if request.URL.Path != home+"/new" && request.Method == http.MethodGet {
		http.Redirect(response, request, home+"/new", http.StatusSeeOther)
		return
	}
	page := portalPage{Mode: "new", Heading: "New request", Action: home + "/new", ListURL: home, Email: email, SignOut: home + "/sign-out"}
	if request.Method != http.MethodPost {
		s.writePortal(response, request, v, page, http.StatusOK)
		return
	}
	fields, ok := portalForm(response, request)
	if !ok {
		return
	}
	if strings.TrimSpace(fields.Get("website")) != "" {
		http.Redirect(response, request, home, http.StatusSeeOther)
		return
	}
	subject, message, problem := portalRequest(fields.Get("subject"), fields.Get("message"))
	if problem != "" {
		page.Error, page.Subject, page.Message = problem, fields.Get("subject"), fields.Get("message")
		s.writePortal(response, request, v, page, http.StatusBadRequest)
		return
	}
	created, err := s.config.Store.CreatePortalTicket(request.Context(), v.site.WorkspaceID, email, portalContactName(email), subject, message)
	if err != nil {
		s.writeError(response, err)
		return
	}
	http.Redirect(response, request, portalTicketPath(v.base, created.Series, created.Number), http.StatusSeeOther)
}

func (s *Site) portalSignOut(response http.ResponseWriter, request *http.Request, v view, home string) {
	if request.Method != http.MethodPost {
		http.Redirect(response, request, home, http.StatusSeeOther)
		return
	}
	if cookie, err := request.Cookie(portalCookieName); err == nil {
		_ = s.config.Store.RevokePortalSession(request.Context(), v.site.WorkspaceID, cookie.Value)
	}
	s.clearPortalCookie(response, v)
	http.Redirect(response, request, home, http.StatusSeeOther)
}

func (s *Site) portalContinue(response http.ResponseWriter, request *http.Request, v view, token, home string) {
	if !portalTokenPattern.MatchString(token) {
		s.writePortal(response, request, v, portalPage{Mode: "note", Heading: "This link does not work",
			Notice: "Ask for a new sign-in link.", Action: home}, http.StatusOK)
		return
	}
	action := home + "/continue/" + token
	if request.Method != http.MethodPost {
		status, err := s.config.Store.PortalLinkStatus(request.Context(), v.site.WorkspaceID, token)
		if err != nil {
			s.writeError(response, err)
			return
		}
		s.writePortal(response, request, v, portalContinuePage(status, action, home), http.StatusOK)
		return
	}
	result, err := s.config.Store.RedeemPortalLink(request.Context(), v.site.WorkspaceID, token)
	if err != nil {
		s.writeError(response, err)
		return
	}
	if result.Status != "ok" {
		s.writePortal(response, request, v, portalContinuePage(result.Status, action, home), http.StatusOK)
		return
	}
	s.setPortalCookie(response, v, result.Session)
	http.Redirect(response, request, home, http.StatusSeeOther)
}

func portalContinuePage(status, action, home string) portalPage {
	switch status {
	case "ready":
		return portalPage{Mode: "continue", Heading: "Continue to your requests", Action: action,
			Notice: "This signs you in on this browser. The link works once."}
	case "used":
		return portalPage{Mode: "note", Heading: "This link was already used", Notice: "Ask for a new sign-in link.", Action: home}
	case "expired":
		return portalPage{Mode: "note", Heading: "This link has expired", Notice: "A link lasts 15 minutes. Ask for a new one.", Action: home}
	default:
		return portalPage{Mode: "note", Heading: "This link does not work", Notice: "Ask for a new sign-in link.", Action: home}
	}
}

func (s *Site) portalTicket(response http.ResponseWriter, request *http.Request, v view, email, home, rest string) {
	if email == "" {
		http.Redirect(response, request, home, http.StatusSeeOther)
		return
	}
	series, number, reply, ok := portalTicketRoute(rest)
	if !ok {
		s.writeSiteNotFound(response, v)
		return
	}
	if reply && request.Method != http.MethodPost {
		http.Redirect(response, request, portalTicketPath(v.base, series, number), http.StatusSeeOther)
		return
	}
	if reply {
		fields, parsed := portalForm(response, request)
		if !parsed {
			return
		}
		message, problem := portalBody(fields.Get("message"), "Write a reply.", "A reply is at most 5,000 characters.")
		if problem != "" {
			s.portalShowTicket(response, request, v, email, home, series, number, problem)
			return
		}
		err := s.config.Store.ReplyToPortalTicket(request.Context(), v.site.WorkspaceID, email, series, number, message)
		if errors.Is(err, store.ErrPortalTicketNotFound) {
			s.writeSiteNotFound(response, v)
			return
		}
		if err != nil {
			s.writeError(response, err)
			return
		}
		http.Redirect(response, request, portalTicketPath(v.base, series, number), http.StatusSeeOther)
		return
	}
	if request.Method == http.MethodPost {
		response.Header().Set("Allow", "GET, HEAD")
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.portalShowTicket(response, request, v, email, home, series, number, "")
}

func (s *Site) portalShowTicket(response http.ResponseWriter, request *http.Request, v view, email, home, series string, number int64, problem string) {
	ticket, err := s.config.Store.PortalTicket(request.Context(), v.site.WorkspaceID, email, series, number)
	if errors.Is(err, store.ErrPortalTicketNotFound) {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	viewTicket := portalTicketView{Ref: ticket.Ref, Subject: ticket.Subject, Status: portalStatus(ticket.Status)}
	for _, message := range ticket.Messages {
		iso, label := portalWhen(message.At)
		by := message.Author
		if message.Kind == "incoming" {
			by = "You"
		} else if by == "" {
			by = "Support"
		}
		viewTicket.Messages = append(viewTicket.Messages, portalMessageView{By: by, Body: message.Body, When: label, WhenISO: iso})
	}
	status := http.StatusOK
	if problem != "" {
		status = http.StatusBadRequest
	}
	s.writePortal(response, request, v, portalPage{Mode: "ticket", Heading: ticket.Subject, Error: problem,
		Action: portalTicketPath(v.base, series, number) + "/reply", ListURL: home, Email: email, SignOut: home + "/sign-out",
		Ticket: viewTicket, MoreMessages: ticket.More}, status)
}

func portalTicketRoute(rest string) (series string, number int64, reply, ok bool) {
	rest = strings.Trim(rest, "/")
	reply = strings.HasSuffix(rest, "/reply")
	rest = strings.TrimSuffix(rest, "/reply")
	parts := strings.Split(rest, "/")
	switch len(parts) {
	case 1:
		number, ok = portalNumber(parts[0])
		return "default", number, reply, ok
	case 2:
		if !portalSeriesPattern.MatchString(parts[0]) {
			return "", 0, false, false
		}
		number, ok = portalNumber(parts[1])
		return parts[0], number, reply, ok
	default:
		return "", 0, false, false
	}
}

func portalNumber(text string) (int64, bool) {
	if text == "" || len(text) > 18 || text[0] < '1' || text[0] > '9' {
		return 0, false
	}
	for _, r := range text[1:] {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	return number, err == nil
}

func (s *Site) writePortal(response http.ResponseWriter, request *http.Request, v view, data portalPage, status int) {
	page := s.page(v, data.Heading, []link{{Name: portalLinkLabel(v.locale()), URL: portalHome(v.base)}})
	page.NoIndex = true
	page.Portal = data
	body := s.pages.render("portal", page)
	header := response.Header()
	securityHeaders(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Set("Cache-Control", "no-store")
	header.Set("X-Robots-Tag", "noindex")
	if status != http.StatusOK {
		header.Del("ETag")
	}
	response.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = response.Write(body)
	}
}

func (s *Site) writePortalWait(response http.ResponseWriter, request *http.Request, v view, wait time.Duration) {
	seconds := int(wait.Seconds()) + 1
	response.Header().Set("Retry-After", strconv.Itoa(seconds))
	s.writePortal(response, request, v, portalPage{Mode: "note", Heading: "Wait a moment",
		Notice: "Too many sign-in emails from here. Try again in a few minutes.", Action: portalHome(v.base)}, http.StatusTooManyRequests)
}

func (s *Site) setPortalCookie(response http.ResponseWriter, v view, token string) {
	http.SetCookie(response, &http.Cookie{Name: portalCookieName, Value: token, Path: portalCookiePath(v.base),
		MaxAge: int(store.PortalSessionLifetime.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func (s *Site) clearPortalCookie(response http.ResponseWriter, v view) {
	http.SetCookie(response, &http.Cookie{Name: portalCookieName, Value: "", Path: portalCookiePath(v.base),
		MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func portalForm(response http.ResponseWriter, request *http.Request) (url.Values, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, portalBodyLimit)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "The form is too large.", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return request.PostForm, true
}

func portalEmailAddress(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "Enter your email address."
	}
	if !email.ValidAddress(value) || strings.ContainsAny(value, "\r\n<>") {
		return "", "Enter a valid email address."
	}
	return strings.ToLower(value), ""
}

func portalRequest(subject, message string) (string, string, string) {
	subject = strings.Join(strings.Fields(strings.ReplaceAll(subject, "\x00", "")), " ")
	if subject == "" {
		return "", "", "Enter a subject."
	}
	if utf8.RuneCountInString(subject) > portalSubjectMax {
		return "", "", "A subject is at most 200 characters."
	}
	body, problem := portalBody(message, "Describe your request.", "A request is at most 5,000 characters.")
	if problem != "" {
		return "", "", problem
	}
	return subject, body, ""
}

func portalBody(message, empty, tooLong string) (string, string) {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\x00", ""))
	if message == "" {
		return "", empty
	}
	if utf8.RuneCountInString(message) > portalMessageMax {
		return "", tooLong
	}
	return message, ""
}

func portalContactName(address string) string {
	local, _, _ := strings.Cut(address, "@")
	local = strings.TrimSpace(local)
	if local == "" || utf8.RuneCountInString(local) > 80 {
		return "Customer"
	}
	return local
}

func portalStatus(status string) string {
	switch status {
	case "pending":
		return "Pending"
	case "resolved":
		return "Resolved"
	case "closed":
		return "Closed"
	default:
		return "Open"
	}
}

func portalWhen(at time.Time) (iso, label string) {
	at = at.UTC()
	return at.Format(time.RFC3339), at.Format("2 Jan 2006, 15:04 UTC")
}
