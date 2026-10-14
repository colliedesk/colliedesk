package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var assets embed.FS

func Handler(api http.Handler) http.Handler {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	served := static
	if _, err := fs.Stat(static, "build/index.html"); err == nil {
		served, _ = fs.Sub(static, "build")
	}
	files := http.FileServer(http.FS(served))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(response, request)
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
