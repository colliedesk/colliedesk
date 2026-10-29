package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// The help center's AI chat as end users reach it: routes of the help chat channel below every help
// center (channels.HelpCenterRoute), never under /api/v1. A help center host never carries the app's
// session, so the chat has its own cookie: a random token per browser and channel, stored as a hash.
// docs/internals/ai-chat.md records the decisions.

const (
	// helpChatPerMinute and helpChatPerHour bound the questions of one client address.
	helpChatPerMinute = 10
	helpChatPerHour   = 60
	// helpChatStreamFor bounds a streamed answer's response, past the server's write timeout.
	helpChatStreamFor = 90 * time.Second
	// helpChatWarmEvery is how often one address may ask a workspace to warm its cache.
	helpChatWarmEvery = 5 * time.Minute
)

// helpChatLimiters bound the chat per client address.
type helpChatLimiters struct {
	once                  sync.Once
	minute, hour, prewarm *webFormLimiter
}

func (l *helpChatLimiters) init() {
	l.once.Do(func() {
		l.minute = newWindowLimiter(helpChatPerMinute, webFormMaxKeys, time.Minute)
		l.hour = newWindowLimiter(helpChatPerHour, webFormMaxKeys, time.Hour)
		l.prewarm = newWindowLimiter(1, webFormMaxKeys, helpChatWarmEvery)
	})
}

// helpChatRoutes are the help chat channel's routes, built around the ai package.
func (a *API) helpChatRoutes() []channels.HelpCenterRoute {
	route := func(method, path string, write bool, serve func(*helpChat)) channels.HelpCenterRoute {
		return channels.HelpCenterRoute{Method: method, Path: path, Handler: func(kit channels.HelpCenterKit) channels.HelpCenterHandler {
			return func(response http.ResponseWriter, request *http.Request, at channels.HelpCenter) {
				chat, ok := a.openHelpChat(kit, response, request, at, write)
				if ok {
					serve(chat)
				}
			}
		}}
	}
	return []channels.HelpCenterRoute{
		route(http.MethodGet, "/chat", false, a.helpChatPage),
		route(http.MethodGet, "/chat/chat.js", false, a.helpChatAsset("chat.js")),
		route(http.MethodGet, "/chat/chat.css", false, a.helpChatAsset("chat.css")),
		route(http.MethodGet, "/chat/widget.js", false, a.helpChatAsset("widget.js")),
		route(http.MethodGet, "/chat/widget-config", false, a.helpChatWidgetConfig),
		route(http.MethodPost, "/chat/transfer", true, a.helpChatTransfer),
		route(http.MethodPost, "/chat/handoff", true, a.helpChatHandoff),
		route(http.MethodGet, "/chat/session", false, a.helpChatSession),
		route(http.MethodPost, "/chat/session/end", true, a.helpChatEnd),
		route(http.MethodPost, "/chat/turns", true, a.helpChatTurn),
		route(http.MethodGet, "/chat/turns/{turn_id}", false, a.helpChatTurnGet),
		route(http.MethodPut, "/chat/turns/{turn_id}/feedback", true, a.helpChatFeedback),
		route(http.MethodDelete, "/chat/turns/{turn_id}/feedback", true, a.helpChatFeedback),
		route(http.MethodPost, "/chat/prewarm", true, a.helpChatPrewarm),
	}
}

// helpChat is one request to the chat: the help center, its channel and surface, and the browser.
type helpChat struct {
	response http.ResponseWriter
	request  *http.Request
	at       channels.HelpCenter
	kit      channels.HelpCenterKit
	channel  channels.Channel
	surface  helpchat.Surface
	// widget: the chat is framed by a site; its cookie is partitioned.
	widget bool
}

// openHelpChat finds the request's channel and checks the chat may answer. A chat that is off (its
// channel or the AI chat feature) is the help center's own 404, as one that is not there. A write must
// come from the help center's own pages (Origin) with JSON.
func (a *API) openHelpChat(kit channels.HelpCenterKit, response http.ResponseWriter, request *http.Request, at channels.HelpCenter, write bool) (*helpChat, bool) {
	ctx := request.Context()
	channel, found, err := kit.Channel(ctx, at.WorkspaceID, request.URL.Query().Get("channel"))
	if err == nil && found && channel.Enabled {
		found, err = a.ai.ChatActive(ctx, at.WorkspaceID)
	}
	if err != nil {
		slog.Error("help chat", "workspace_id", at.WorkspaceID, "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "Something went wrong. Try again in a moment.")
		return nil, false
	}
	if !found || !channel.Enabled {
		at.NotFound(response)
		return nil, false
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if write {
		if origin := request.Header.Get("Origin"); origin == "" || !strings.EqualFold(origin, helpCenterOrigin(at.Address)) {
			writeError(response, http.StatusForbidden, "csrf_rejected", "This request did not come from the help center's own page.")
			return nil, false
		}
		if request.ContentLength != 0 && request.Body != http.NoBody {
			if kind, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type")); kind != "application/json" {
				writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send JSON.")
				return nil, false
			}
		}
	}
	config, err := a.store.ChannelConfigOf(ctx, channel)
	if err != nil {
		slog.Error("help chat surface", "workspace_id", at.WorkspaceID, "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "Something went wrong. Try again in a moment.")
		return nil, false
	}
	surface, _ := config.(helpchat.Surface)
	return &helpChat{response: response, request: request, at: at, kit: kit, channel: channel, surface: surface,
		widget: request.URL.Query().Get("surface") == ai.SurfaceWidget}, true
}

// helpCenterOrigin is scheme://host[:port] of a help center address.
func helpCenterOrigin(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// cookie is the chat's cookie of this help center and channel: on a host of its own __Host- with
// Path=/, below /help/<slug> __Secure- with its path, and in a site's frame partitioned (CHIPS). A
// server on plain http (development) gets neither prefix nor Secure. Never Domain=.
func (c *helpChat) cookie(widget bool) *http.Cookie {
	channel8 := c.channel.ID
	if len(channel8) > 8 {
		channel8 = channel8[:8]
	}
	name, path := "cd_chat_", "/"
	if widget {
		name += "w_"
	}
	if c.at.Base != "" {
		name += c.at.Slug + "_"
		path = c.at.Base
	}
	name += channel8
	secure := !strings.HasPrefix(c.at.Address, "http://")
	if secure {
		if c.at.Base == "" {
			name = "__Host-" + name
		} else {
			name = "__Secure-" + name
		}
	}
	cookie := &http.Cookie{Name: name, Path: path, Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(ai.ChatKeptFor.Seconds())}
	if widget && secure {
		cookie.SameSite, cookie.Partitioned = http.SameSiteNoneMode, true
	}
	return cookie
}

// browser is the chat the request's cookie names; token is empty without one. A widget reads its
// partitioned cookie first, then the help center's own (a browser that sends it).
func (c *helpChat) browser() (ai.ChatBrowser, string) {
	names := []string{c.cookie(false).Name}
	if c.widget {
		names = append([]string{c.cookie(true).Name}, names...)
	}
	for _, name := range names {
		if cookie, err := c.request.Cookie(name); err == nil && cookie.Value != "" {
			return ai.ChatBrowser{ChannelID: c.channel.ID, TokenHash: tokenHash(cookie.Value)}, cookie.Value
		}
	}
	return ai.ChatBrowser{ChannelID: c.channel.ID}, ""
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (c *helpChat) setCookie(token string, remove bool) {
	cookie := c.cookie(c.widget)
	cookie.Value = token
	if remove {
		cookie.Value, cookie.MaxAge = "", -1
	}
	http.SetCookie(c.response, cookie)
}

func (c *helpChat) chatSurface() ai.ChatSurface {
	return ai.ChatSurface{ChannelID: c.channel.ID, WorkspaceName: c.at.Name, Instructions: c.surface.Instructions}
}

// sourceView is a cited article as the browser shows it, with its address on the help center.
type helpChatSource struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Folder string `json:"folder"`
}

func (c *helpChat) source(number int64, title, folder string) helpChatSource {
	return helpChatSource{Number: number, Title: title, Folder: folder, URL: c.at.Address + "/a/" + strconv.FormatInt(number, 10)}
}

func (a *API) helpChatSession(c *helpChat) {
	browser, token := c.browser()
	if token == "" {
		c.response.WriteHeader(http.StatusNoContent)
		return
	}
	view, found, err := a.ai.ChatSessionOf(c.request.Context(), c.at.WorkspaceID, browser)
	if err != nil {
		a.helpChatFailed(c, err)
		return
	}
	if !found {
		c.response.WriteHeader(http.StatusNoContent)
		return
	}
	turns := make([]map[string]any, 0, len(view.Turns))
	for _, turn := range view.Turns {
		turns = append(turns, c.turnBody(turn))
	}
	writeJSON(c.response, http.StatusOK, map[string]any{"status": view.Status, "idle_hours": view.IdleHours, "turns": turns})
}

func (c *helpChat) turnBody(turn ai.ChatTurnView) map[string]any {
	sources := make([]helpChatSource, 0, len(turn.Sources))
	for _, source := range turn.Sources {
		sources = append(sources, c.source(source.Number, source.Title, source.Folder))
	}
	return map[string]any{"id": turn.ID, "question": turn.Question, "answer": turn.Answer, "status": turn.Status, "error_code": turn.ErrorCode,
		"sources": sources, "feedback": turn.Feedback, "redacted": turn.Redacted, "created_at": turn.CreatedAt}
}

func (a *API) helpChatTurnGet(c *helpChat) {
	browser, _ := c.browser()
	turn, err := a.ai.ChatTurnOf(c.request.Context(), c.at.WorkspaceID, browser, c.request.PathValue("turn_id"))
	if errors.Is(err, ai.ErrChatTurnNotFound) {
		writeError(c.response, http.StatusNotFound, "not_found", "No such answer in this chat.")
		return
	}
	if err != nil {
		a.helpChatFailed(c, err)
		return
	}
	writeJSON(c.response, http.StatusOK, c.turnBody(turn))
}

func (a *API) helpChatFeedback(c *helpChat) {
	rating := ""
	if c.request.Method == http.MethodPut {
		var input struct {
			Rating string `json:"rating"`
		}
		if err := decodeJSON(c.request, &input); err != nil || (input.Rating != "helpful" && input.Rating != "not_helpful") {
			writeError(c.response, http.StatusUnprocessableEntity, "invalid_request", "rating is helpful or not_helpful.")
			return
		}
		rating = input.Rating
	}
	browser, _ := c.browser()
	err := a.ai.SetChatFeedback(c.request.Context(), c.at.WorkspaceID, browser, c.request.PathValue("turn_id"), rating)
	if errors.Is(err, ai.ErrChatTurnNotFound) {
		writeError(c.response, http.StatusNotFound, "not_found", "No such answer in this chat.")
		return
	}
	if err != nil {
		a.helpChatFailed(c, err)
		return
	}
	c.response.WriteHeader(http.StatusNoContent)
}

func (a *API) helpChatEnd(c *helpChat) {
	browser, token := c.browser()
	if token != "" {
		if err := a.ai.EndChat(c.request.Context(), c.at.WorkspaceID, browser); err != nil {
			a.helpChatFailed(c, err)
			return
		}
	}
	c.setCookie("", true)
	c.response.WriteHeader(http.StatusNoContent)
}

// helpChatPrewarm warms the provider's cache of the articles when the end user starts typing, at most
// once in 5 minutes per address and workspace; it creates no chat. It always answers 204.
func (a *API) helpChatPrewarm(c *helpChat) {
	a.helpChat.init()
	key := clientAddress(c.request, a.config.TrustedProxies) + "|" + c.at.WorkspaceID
	if allowed, _ := a.helpChat.prewarm.allow(key, a.now()); allowed {
		if err := a.ai.ChatPrewarm(c.request.Context(), c.at.WorkspaceID, c.chatSurface()); err != nil && !errors.Is(err, ai.ErrFeatureOff) {
			slog.Warn("help chat pre-warm", "workspace_id", c.at.WorkspaceID, "error", err)
		}
	}
	c.response.WriteHeader(http.StatusNoContent)
}

// helpChatTurn answers a question as NDJSON, one event a line: turn, delta*, sources, done (or error).
func (a *API) helpChatTurn(c *helpChat) {
	var input struct {
		Question       string `json:"question"`
		ViewingArticle int64  `json:"viewing_article_number"`
		Surface        string `json:"surface"`
	}
	if err := json.NewDecoder(io.LimitReader(c.request.Body, 64<<10)).Decode(&input); err != nil {
		writeError(c.response, http.StatusUnprocessableEntity, "invalid_request", "Send the question as JSON.")
		return
	}
	a.helpChat.init()
	client, now := clientAddress(c.request, a.config.TrustedProxies), a.now()
	for _, limiter := range []*webFormLimiter{a.helpChat.minute, a.helpChat.hour} {
		if allowed, wait := limiter.allow(client, now); !allowed {
			c.response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(c.response, http.StatusTooManyRequests, "rate_limited", "Too many questions. Wait a minute and ask again.")
			return
		}
	}
	browser, token := c.browser()
	if token == "" {
		token = newChatToken()
		browser.TokenHash = tokenHash(token)
	}
	started := input.Surface
	if c.widget {
		started = ai.SurfaceWidget
	}
	stream := &helpChatStream{chat: c, token: token}
	err := a.ai.ChatTurn(c.request.Context(), c.at.WorkspaceID, ai.ChatTurnInput{Surface: c.chatSurface(), Browser: browser, Started: started,
		Question: input.Question, ViewingArticle: input.ViewingArticle}, stream)
	if stream.started {
		if err != nil {
			slog.Error("help chat turn", "workspace_id", c.at.WorkspaceID, "error", err)
		}
		return
	}
	var invalid *ai.InvalidError
	switch {
	case err == nil:
	case errors.Is(err, ai.ErrFeatureOff):
		c.at.NotFound(c.response)
	case errors.Is(err, ai.ErrChatBusy):
		c.response.Header().Set("Retry-After", "5")
		writeError(c.response, http.StatusTooManyRequests, "rate_limited", "Too many questions at once. Ask again in a moment.")
	case errors.Is(err, ai.ErrChatFull):
		writeError(c.response, http.StatusConflict, "chat_full", "This chat is long. Start a new chat to keep asking.")
	case errors.As(err, &invalid):
		code := "invalid_request"
		if len([]rune(strings.TrimSpace(input.Question))) > ai.ChatQuestionRunes {
			code = "question_too_long"
		}
		writeError(c.response, http.StatusUnprocessableEntity, code, invalid.Message)
	default:
		a.helpChatFailed(c, err)
	}
}

func newChatToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (a *API) helpChatFailed(c *helpChat, err error) {
	slog.Error("help chat", "workspace_id", c.at.WorkspaceID, "error", err)
	writeError(c.response, http.StatusInternalServerError, "internal_error", "Something went wrong. Try again in a moment.")
}

// helpChatStream writes an answer's events as NDJSON. Its headers, the chat's cookie (renewed for 30
// days) among them, go with the first event; a write after the end user left fails quietly.
type helpChatStream struct {
	chat    *helpChat
	token   string
	started bool
	gone    bool
}

func (s *helpChatStream) line(event map[string]any) {
	response := s.chat.response
	controller := http.NewResponseController(response)
	if !s.started {
		s.started = true
		s.chat.setCookie(s.token, false)
		header := response.Header()
		header.Set("Content-Type", "application/x-ndjson")
		header.Set("Cache-Control", "no-store, no-transform")
		header.Set("X-Accel-Buffering", "no")
		// A recorder in tests cannot set deadlines (http.ErrNotSupported); a real connection can.
		_ = controller.SetWriteDeadline(time.Now().Add(helpChatStreamFor))
		response.WriteHeader(http.StatusOK)
	}
	if s.gone {
		return
	}
	data, _ := json.Marshal(event)
	if _, err := response.Write(append(data, '\n')); err != nil {
		s.gone = true
		return
	}
	if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.gone = true
	}
}

func (s *helpChatStream) Turn(id string, created bool) {
	s.line(map[string]any{"type": "turn", "turn_id": id, "session_created": created})
}

func (s *helpChatStream) Delta(text string) { s.line(map[string]any{"type": "delta", "text": text}) }

func (s *helpChatStream) Sources(sources []ai.ChatArticleRef) {
	list := make([]helpChatSource, 0, len(sources))
	for _, source := range sources {
		list = append(list, s.chat.source(source.Number, source.Title, source.Folder))
	}
	s.line(map[string]any{"type": "sources", "sources": list})
}

func (s *helpChatStream) Done(status string, links []string, redacted bool) {
	if links == nil {
		links = []string{}
	}
	s.line(map[string]any{"type": "done", "status": status, "links_allowed": links, "redacted": redacted})
}

func (s *helpChatStream) Error(code string) { s.line(map[string]any{"type": "error", "code": code}) }

// helpChatConfig is what chat.js needs of the page it runs on (its data-config).
func (c *helpChat) config(surface string) string {
	look := c.at.Look
	config := map[string]any{"surface": surface, "name": c.at.Name, "siteTitle": look.SiteTitle, "home": look.Home, "search": look.SearchURL,
		"api": c.at.Base + "/chat", "channel": c.channel.ID, "page": c.at.Base + "/chat?channel=" + url.QueryEscape(c.channel.ID), "trial": c.at.Trial,
		"greeting": c.surface.Greeting, "suggestions": c.surface.Suggestions}
	if look.LogoURL != "" {
		config["logo"] = map[string]any{"url": look.LogoURL, "alt": look.LogoAlt, "width": look.LogoWidth, "height": look.LogoHeight}
	}
	data, _ := json.Marshal(config)
	return string(data)
}

func (c *helpChat) assetPath(name string) string {
	file, _ := helpchat.Asset(name)
	return c.at.Base + "/chat/" + name + "?v=" + file.Version
}

// helpChatPage is the chat page (/chat), or the chat in a site's frame (/chat?embed=1). A one-time code
// (?t=, from the frame's Open full chat) moves the frame's chat to this page's own cookie first.
func (a *API) helpChatPage(c *helpChat) {
	query := c.request.URL.Query()
	if code := query.Get("t"); code != "" {
		token := newChatToken()
		moved, err := a.ai.RedeemChatTransfer(c.request.Context(), c.at.WorkspaceID, c.channel.ID, tokenHash(code), tokenHash(token))
		if err != nil {
			a.helpChatFailed(c, err)
			return
		}
		if moved {
			c.setCookie(token, false)
		}
		// The code leaves the address bar and the history.
		http.Redirect(c.response, c.request, c.at.Base+"/chat?channel="+url.QueryEscape(c.channel.ID), http.StatusSeeOther)
		return
	}
	embed := query.Get("embed") == "1"
	surface, ancestors := "page", ""
	if embed {
		surface = "embed"
		ancestors = strings.Join(c.surface.WidgetOrigins, " ")
	}
	body := helpchat.RenderPage(helpchat.Page{Look: c.at.Look, Title: "Ask " + c.at.Name, Embed: embed, Config: c.config(surface),
		Script: c.assetPath("chat.js"), Style: c.assetPath("chat.css")})
	header := c.response.Header()
	header.Set("Content-Security-Policy", helpcenter.ScriptPolicy(c.at.Address+"/chat/chat.js", ancestors))
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("X-Robots-Tag", "noindex")
	if !embed {
		header.Set("X-Frame-Options", "DENY")
	}
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	c.response.WriteHeader(http.StatusOK)
	if c.request.Method != http.MethodHead {
		_, _ = c.response.Write(body)
	}
}

// helpChatAsset serves one of the chat's files. Its address carries its version (?v=), so that one is
// kept for a year; widget.js, whose address a site's page holds, is checked again after an hour.
func (a *API) helpChatAsset(name string) func(*helpChat) {
	return func(c *helpChat) {
		file, _ := helpchat.Asset(name)
		header := c.response.Header()
		etag := `"` + file.Version + `"`
		header.Set("Content-Type", file.ContentType)
		header.Set("ETag", etag)
		switch {
		case c.request.URL.Query().Get("v") == file.Version:
			header.Set("Cache-Control", "public, max-age=31536000, immutable")
		case name == "widget.js":
			header.Set("Cache-Control", "public, max-age=3600")
		default:
			header.Set("Cache-Control", "no-cache")
		}
		if c.request.Header.Get("If-None-Match") == etag {
			c.response.WriteHeader(http.StatusNotModified)
			return
		}
		header.Set("Content-Length", strconv.Itoa(len(file.Body)))
		c.response.WriteHeader(http.StatusOK)
		if c.request.Method != http.MethodHead {
			_, _ = c.response.Write(file.Body)
		}
	}
}

// helpChatWidgetConfig tells widget.js on a company's site what to draw: only for a site the channel
// lists (its Origin), with CORS for that site alone; any other site gets the help center's 404 and no
// CORS header, so the widget draws nothing. brand is the help center's accent colors, null without.
func (a *API) helpChatWidgetConfig(c *helpChat) {
	origin := strings.ToLower(c.request.Header.Get("Origin"))
	if origin == "" || !slices.Contains(c.surface.WidgetOrigins, origin) {
		c.at.NotFound(c.response)
		return
	}
	var brand any
	if c.at.Look.Accent != "" {
		brand = map[string]string{"accent": c.at.Look.Accent, "accent_ink": c.at.Look.AccentInk}
	}
	header := c.response.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	header.Add("Vary", "Origin")
	writeJSON(c.response, http.StatusOK, map[string]any{"name": c.at.Name, "greeting": c.surface.Greeting, "suggestions": c.surface.Suggestions, "brand": brand})
}

// helpChatTransfer makes a one-time code that moves the frame's chat to the help center's own page.
func (a *API) helpChatTransfer(c *helpChat) {
	browser, token := c.browser()
	code := newChatToken()
	found := false
	if token != "" {
		var err error
		if found, err = a.ai.CreateChatTransfer(c.request.Context(), c.at.WorkspaceID, browser, tokenHash(code)); err != nil {
			a.helpChatFailed(c, err)
			return
		}
	}
	page := c.at.Address + "/chat?channel=" + url.QueryEscape(c.channel.ID)
	if found {
		page += "&t=" + url.QueryEscape(code)
	}
	writeJSON(c.response, http.StatusOK, map[string]string{"url": page})
}

// helpChatAdditions is what the help chat adds to the help center's own pages while its default channel
// and the AI chat feature are on: Ask AI in the header and on the home page, and an article's chat box.
func (a *API) helpChatAdditions(kit channels.HelpCenterKit) func(ctx context.Context, at channels.HelpCenter) (channels.HelpCenterAdditions, error) {
	return func(ctx context.Context, at channels.HelpCenter) (channels.HelpCenterAdditions, error) {
		channel, found, err := kit.Channel(ctx, at.WorkspaceID, "")
		if err != nil || !found || !channel.Enabled {
			return channels.HelpCenterAdditions{}, err
		}
		if on, err := a.ai.ChatActive(ctx, at.WorkspaceID); err != nil || !on {
			return channels.HelpCenterAdditions{}, err
		}
		config, err := a.store.ChannelConfigOf(ctx, channel)
		if err != nil {
			return channels.HelpCenterAdditions{}, err
		}
		surface, _ := config.(helpchat.Surface)
		c := &helpChat{at: at, channel: channel, surface: surface}
		box := c.config("article")
		js, _ := helpchat.Asset("chat.js")
		css, _ := helpchat.Asset("chat.css")
		sum := sha256.Sum256([]byte(box + js.Version + css.Version))
		return channels.HelpCenterAdditions{Link: "/chat", LinkLabel: "Ask AI", ArticleStyle: "/chat/chat.css?v=" + css.Version, ArticleScript: "/chat/chat.js?v=" + js.Version,
			ArticleBox: true, BoxConfig: box, Version: hex.EncodeToString(sum[:8])}, nil
	}
}

// helpChatHandoff is "Talk to a person": the chat, as text with the articles it cited, goes to the team
// as an inbound message of the help chat channel (the shared ingest: rate limits by address, threading
// by chat, trial rules). A second hand-off of the same chat joins its conversation. The address was
// typed and is never checked; the inbox says so.
func (a *API) helpChatHandoff(c *helpChat) {
	var input struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Note  string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(c.request.Body, 16<<10)).Decode(&input); err != nil {
		writeError(c.response, http.StatusBadRequest, "invalid_request", "The body must be a JSON object with an email.")
		return
	}
	submission, fields := validateWebForm(input.Email, "", "")
	if fields == nil {
		fields = map[string]string{}
	}
	input.Name = strings.Join(strings.Fields(input.Name), " ")
	if utf8.RuneCountInString(input.Name) > 120 {
		fields["name"] = "Name is too long: at most 120 characters."
	}
	if utf8.RuneCountInString(input.Note) > 2000 {
		fields["note"] = "Note is too long: at most 2,000 characters."
	}
	if len(fields) > 0 {
		writeJSON(c.response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(c.response, map[string]any{
			"code": "validation_failed", "message": webFormFieldsMessage(fields), "fields": fields})})
		return
	}
	ctx := c.request.Context()
	handoff := helpchat.Handoff{Email: submission.Email, Name: input.Name, Note: strings.TrimSpace(input.Note),
		Address: helpchat.AddressKey(c.at.WorkspaceID, clientAddress(c.request, a.config.TrustedProxies))}
	if browser, token := c.browser(); token != "" {
		view, found, err := a.ai.ChatSessionOf(ctx, c.at.WorkspaceID, browser)
		if err != nil {
			a.helpChatFailed(c, err)
			return
		}
		if found {
			handoff.SessionID, handoff.Transcript = view.ID, c.transcript(view)
			if len(view.Turns) > 0 && view.Turns[0].Question != nil {
				handoff.FirstQuestion = *view.Turns[0].Question
			}
		}
	}
	if handoff.Transcript == "" {
		handoff.Transcript = "(No new questions.)"
	}
	body, err := json.Marshal(handoff)
	if err != nil {
		a.helpChatFailed(c, err)
		return
	}
	results, err := c.kit.Receive(ctx, c.channel, []channels.Raw{{JSON: body, ReceivedAt: a.now()}})
	switch {
	case errors.Is(err, channels.ErrChannelOff), errors.Is(err, domain.ErrMailboxClosed):
		c.at.NotFound(c.response)
		return
	case errors.Is(err, store.ErrRateLimited):
		c.response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(c.response, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later.")
		return
	case err != nil:
		a.helpChatFailed(c, err)
		return
	}
	conversationID := ""
	for _, result := range results {
		if result.ConversationID != nil {
			conversationID = *result.ConversationID
		}
	}
	if conversationID == "" {
		a.helpChatFailed(c, fmt.Errorf("hand-off stored no conversation"))
		return
	}
	if err := a.ai.ChatHandedOff(ctx, c.at.WorkspaceID, handoff.SessionID, conversationID); err != nil {
		a.helpChatFailed(c, err)
		return
	}
	writeJSON(c.response, http.StatusCreated, map[string]any{"reference": chatReference(conversationID), "trial": c.at.Trial})
}

// chatReference is what the end user may quote to the team: the start of the conversation's id (the
// conversation's address in the app begins with it). Conversations have no number of their own.
func chatReference(conversationID string) string {
	return strings.ToUpper(strings.ReplaceAll(conversationID, "-", "")[:8])
}

// chatMarker is an answer's citation of an article ([#12]): the transcript lists the sources instead.
var chatMarker = regexp.MustCompile(`\s*\[#\d+\]`)

// transcript is the chat as the team reads it: each question and answer, with the articles cited.
func (c *helpChat) transcript(view ai.ChatSessionView) string {
	// Each line a paragraph of its own: the inbox shows plain text by paragraphs. After a first hand-off
	// only the turns since go: the conversation already holds the rest.
	var text strings.Builder
	if view.HandedOffAt != nil {
		text.WriteString("Since the last message:\n\n")
	}
	turns := 0
	for _, turn := range view.Turns {
		if view.HandedOffAt != nil && !turn.CreatedAt.After(*view.HandedOffAt) {
			continue
		}
		turns++
		if turn.Question != nil {
			text.WriteString("Customer: " + *turn.Question + "\n\n")
		}
		answer := "(no answer)"
		switch {
		case turn.Status == "over_limit":
			answer = "(AI answers were paused: over the chat's limit)"
		case turn.Answer != nil && strings.TrimSpace(*turn.Answer) != "":
			answer = strings.TrimSpace(chatMarker.ReplaceAllString(*turn.Answer, ""))
		}
		text.WriteString("AI: " + answer + "\n\n")
		for _, source := range turn.Sources {
			s := c.source(source.Number, source.Title, source.Folder)
			link := s.URL
			if strings.HasPrefix(link, "/") {
				link = c.at.Address + link
			}
			text.WriteString("Source: " + s.Title + " " + link + "\n\n")
		}
	}
	if turns == 0 {
		return ""
	}
	return strings.TrimSpace(text.String())
}
