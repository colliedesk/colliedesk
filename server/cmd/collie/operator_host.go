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
func operatorHost(appOrigin, operatorOrigin string, next http.Handler) http.Handler {
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
		operatorRoute := strings.HasPrefix(path, "/api/v1/operator/") || path == mcpserver.OperatorPath
		if strings.ToLower(request.Host) == host {
			if operatorRoute {
				next.ServeHTTP(response, request)
				return
			}
			writeOperatorElsewhere(response, http.StatusNotFound, "not_found", "The operator console's host serves only the console.")
			return
		}
		if operatorRoute || path == "/operator" || strings.HasPrefix(path, "/operator/") {
			writeOperatorElsewhere(response, http.StatusNotFound, "operator_console_elsewhere", "The operator console has its own address: "+operatorOrigin)
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
