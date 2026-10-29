package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// operatorHost serves the operator console on its own host when OPERATOR_ORIGIN is set
// (docs/self-host/operator-console.md). It sits outside the help center, which would otherwise take any
// host that is not the app's, or read "ops" of ops.<help center domain> as a workspace slug. Each host
// serves only its own routes: the operator host the operator API, the operator MCP endpoint and the
// console; the app host none of them. Without OPERATOR_ORIGIN everything is served on the app host.
// next is the app (help center first); operatorAPI is the same without the help center, which would take the
// console's host for a help center host.
func operatorHost(appOrigin, operatorOrigin string, next, operatorAPI, console http.Handler) http.Handler {
	if operatorOrigin == "" {
		return next
	}
	parsed, err := url.Parse(operatorOrigin)
	if err != nil {
		return next
	}
	host := strings.ToLower(parsed.Host)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		decision := strings.HasPrefix(path, "/api/v1/agent-requests/") && (strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/reject"))
		realm := request.Header.Get("X-Colliedesk-Realm") == "operator"
		operatorRoute := strings.HasPrefix(path, "/api/v1/operator/") || path == mcpserver.OperatorPath || (decision && realm)
		if strings.ToLower(request.Host) == host {
			switch {
			case operatorRoute:
				operatorAPI.ServeHTTP(response, request)
			case strings.HasPrefix(path, "/api/") || path == mcpserver.Path:
				writeOperatorElsewhere(response, http.StatusNotFound, "not_found", "The operator console's host serves only the console.")
			default:
				console.ServeHTTP(response, request)
			}
			return
		}
		if operatorRoute {
			writeOperatorElsewhere(response, http.StatusNotFound, "operator_console_elsewhere", "The operator console has its own address: "+operatorOrigin)
			return
		}
		if path == "/operator" || strings.HasPrefix(path, "/operator/") {
			writeWrongHost(response, request.Host, operatorOrigin)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func writeOperatorElsewhere(response http.ResponseWriter, status int, code, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_, _ = response.Write([]byte(`{"error":{"code":"` + code + `","message":` + jsonString(message) + `}}`))
}

func jsonString(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(text) + `"`
}

// writeWrongHost is the page the app's host answers for /operator/ once the console has its own address: it
// names the app's host, not the console, and links to the console (design ops-uiux, wrong-host).
func writeWrongHost(response http.ResponseWriter, host, operatorOrigin string) {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	response.WriteHeader(http.StatusNotFound)
	_, _ = response.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>Colliedesk</title><style>body{margin:0;font:16px/1.5 system-ui,sans-serif;color:#0a0a0a;background:#fafafa}header{background:#0a0a0a;color:#fafafa;padding:12px 16px}` +
		`main{max-width:36rem;margin:48px auto;padding:0 16px}a{color:inherit}</style></head><body><header>Colliedesk · ` + escape.Replace(host) + `</header>` +
		`<main><h1>The operator console has its own address</h1><p>Open <a href="` + escape.Replace(operatorOrigin) + `">` + escape.Replace(operatorOrigin) + `</a>. ` +
		`A sign-in link printed with collie operator pair points there.</p></main></body></html>`))
}
