package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
)

//go:embed static
var assets embed.FS

// ConsoleSignIn is what the console's sign-in page needs of cloud sign-in; the zero value when it is off.
type ConsoleSignIn struct {
	PublishableKey string
}

func Handler(api http.Handler, signIn ConsoleSignIn) http.Handler {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	served := static
	if _, err := fs.Stat(static, "build/index.html"); err == nil {
		served, _ = fs.Sub(static, "build")
	}
	files := http.FileServer(http.FS(served))
	console := OperatorHandler("/operator", signIn)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(response, request)
			return
		}
		if request.URL.Path == "/operator" || strings.HasPrefix(request.URL.Path, "/operator/") {
			console.ServeHTTP(response, request)
			return
		}
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" {
			if file, err := served.Open(path); err == nil {
				_ = file.Close()
				files.ServeHTTP(response, request)
				return
			}
		}
		clone := request.Clone(request.Context())
		clone.URL.Path = "/"
		files.ServeHTTP(response, clone)
	})
}

// operatorCSP is the console's content policy: its own scripts, styles and fonts only (no inline script, so the
// theme is set by /operator/theme-boot.js), images from itself and data: (the QR code), no framing.
const operatorCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; form-action 'self'; base-uri 'none'"

// signInCSP is the sign-in page's policy when the operator signs in with cloud sign-in: the console's, plus
// what the identity provider's form needs (its Frontend API host, bot protection, inline styles; Clerk's
// list, clerk.com/docs/security/clerk-csp). Only this page gets it: after a cloud sign-in the console loads
// itself again, so every signed-in screen runs under operatorCSP.
func signInCSP(frontendHost string) string {
	host := "https://" + frontendHost
	return "default-src 'self'; script-src 'self' " + host + " https://challenges.cloudflare.com https://*.protect.clerk.com; " +
		"connect-src 'self' " + host + " https://*.protect.clerk.com:*; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://img.clerk.com; " +
		"frame-src https://challenges.cloudflare.com https://*.protect.clerk.com; worker-src 'self' blob:; " +
		"frame-ancestors 'none'; form-action 'self'; base-uri 'none'"
}

// OperatorHandler serves the operator console (its own Vite build, build/operator). Its files live under
// /operator/ on every host; every other path is the console's page, told where its routes start: base is
// "/operator" on the app's host and "" on OPERATOR_ORIGIN.
// With signIn's publishable key every screen names it (colliedesk-operator-clerk) and the sign-in page gets
// signInCSP.
func OperatorHandler(base string, signIn ConsoleSignIn) http.Handler {
	static, err := fs.Sub(assets, "static/build/operator")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/operator", http.FileServer(http.FS(static)))
	page, pageErr := fs.ReadFile(static, "operator.html")
	if pageErr == nil {
		page = bytes.Replace(page, []byte(`name="colliedesk-operator-base" content="/operator"`), []byte(`name="colliedesk-operator-base" content="`+base+`"`), 1)
	}
	frontendHost := cloudauth.FrontendHost(signIn.PublishableKey)
	if frontendHost != "" && pageErr == nil {
		// Every screen names the key (public, as /api/v1/config serves it to the app): signing out and an
		// ended session lead to the cloud sign-in. The key matched pk_(live|test)_[A-Za-z0-9+/=]+, so it
		// needs no escaping in an attribute.
		page = bytes.Replace(page, []byte(`name="colliedesk-operator-clerk" content=""`), []byte(`name="colliedesk-operator-clerk" content="`+signIn.PublishableKey+`"`), 1)
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		if strings.HasPrefix(path, "/operator/assets/") || path == "/operator/theme-boot.js" || path == "/operator/favicon.svg" {
			files.ServeHTTP(response, request)
			return
		}
		if !consolePage(strings.TrimPrefix(path, base)) {
			http.NotFound(response, request)
			return
		}
		header := response.Header()
		policy := operatorCSP
		if frontendHost != "" && strings.TrimPrefix(path, base) == "/sign-in" {
			policy = signInCSP(frontendHost)
		}
		header.Set("Content-Security-Policy", policy)
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		if pageErr != nil {
			http.Error(response, "The operator console is not in this build: run corepack pnpm build:web in web/.", http.StatusNotFound)
			return
		}
		header.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write(page)
	})
}

// consolePage reports whether path (under the console's base) is one of its screens; others are 404, so the
// console's host never looks like it serves the workspace app.
func consolePage(path string) bool {
	switch path {
	case "", "/", "/sign-in", "/requests", "/history", "/audit", "/operators":
		return true
	}
	return strings.HasPrefix(path, "/config/") || strings.HasPrefix(path, "/requests/") || strings.HasPrefix(path, "/history/")
}
