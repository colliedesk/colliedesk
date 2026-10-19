// Command google-stand-in serves googletest, the local stand-in for Google's consent page, token endpoint
// and Gmail send, so Gmail connect can be tried and tested without a Google account or credential. Point
// an end-to-end build of collie at it with COLLIEDESK_E2E_GOOGLE_URL. The consent page asks which address
// to approve as; messages "sent" are listed at GET /stand-in/messages.
//
//	go run ./cmd/google-stand-in -listen 127.0.0.1:8096 -client-id stand-in-client -client-secret stand-in-secret
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8096", "listen address")
	clientID := flag.String("client-id", "stand-in-client", "OAuth client id the server is configured with")
	clientSecret := flag.String("client-secret", "stand-in-secret", "OAuth client secret the server is configured with")
	flag.Parse()
	fake := googletest.New(*clientID, *clientSecret)
	mux := http.NewServeMux()
	mux.Handle("/", fake)
	mux.HandleFunc("GET /stand-in/messages", func(response http.ResponseWriter, _ *http.Request) {
		type listed struct {
			Account   string `json:"account"`
			MessageID string `json:"message_id"`
			Raw       string `json:"raw"`
		}
		var items []listed
		for _, sent := range fake.Messages() {
			items = append(items, listed{Account: sent.Account, MessageID: sent.MessageID, Raw: string(sent.Raw)})
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(items)
	})
	// Plays the person removing Colliedesk's access in their Google account.
	mux.HandleFunc("POST /stand-in/revoke-all", func(response http.ResponseWriter, _ *http.Request) {
		fake.RevokeAll()
		response.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Google stand-in on http://%s (client id %s)", *listen, *clientID)
	log.Fatal(server.ListenAndServe())
}
