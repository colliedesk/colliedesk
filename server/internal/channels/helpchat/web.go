package helpchat

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"log"

	"github.com/colliedesk/colliedesk/server/internal/channels"
)

// The end user's side of the chat: its page, the script and style sheet the page and an article's chat
// box load, and the script a company's site loads (the widget). They are the help center's own files:
// no other origin, no inline script or style, and every DOM write through textContent (Trusted Types).

//go:embed web/chat.js web/chat.css web/widget.js web/page.html
var web embed.FS

// File is one of the chat's files, with a version that names its bytes.
type File struct {
	Body        []byte
	ContentType string
	Version     string
}

var files = func() map[string]File {
	out := map[string]File{}
	for name, kind := range map[string]string{"chat.js": "text/javascript; charset=utf-8", "chat.css": "text/css; charset=utf-8", "widget.js": "text/javascript; charset=utf-8"} {
		body, err := web.ReadFile("web/" + name)
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(body)
		out[name] = File{Body: body, ContentType: kind, Version: hex.EncodeToString(sum[:6])}
	}
	return out
}()

var page = template.Must(template.ParseFS(web, "web/page.html"))

// Asset is one of the chat's files by name: chat.js, chat.css or widget.js.
func Asset(name string) (File, bool) {
	file, ok := files[name]
	return file, ok
}

// Page is the chat page: the help center's look, whether a site frames it, and what chat.js needs (JSON).
type Page struct {
	Look          channels.HelpCenterLook
	Title         string
	Embed         bool
	Config        string
	Script, Style string
}

// RenderPage renders the chat page.
func RenderPage(p Page) []byte {
	var out bytes.Buffer
	if err := page.Execute(&out, p); err != nil {
		log.Printf("help chat: render the chat page: %v", err)
		return []byte("<!doctype html><title>Error</title><p>This page could not be shown.</p>")
	}
	return out.Bytes()
}
