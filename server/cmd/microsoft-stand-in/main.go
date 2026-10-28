// Command microsoft-stand-in serves microsofttest, the local stand-in for Microsoft's sign-in and consent
// pages, token endpoint, admin consent page and Graph's /me and sendMail, so Microsoft 365 connect can be
// tried and tested without a Microsoft account or credential. Point an end-to-end build of collie at it
// with COLLIEDESK_E2E_MICROSOFT_URL. The sign-in page asks which account to approve as and whether it is a
// work or personal account; messages "sent" are listed at GET /stand-in/messages.
//
//	go run ./cmd/microsoft-stand-in -listen 127.0.0.1:8097 -client-id stand-in-client -client-secret stand-in-secret
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/microsoft/microsofttest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8097", "listen address")
	clientID := flag.String("client-id", "stand-in-client", "application (client) id the server is configured with")
	clientSecret := flag.String("client-secret", "stand-in-secret", "client secret the server is configured with")
	flag.Parse()
	fake := microsofttest.New(*clientID, *clientSecret)
	mux := http.NewServeMux()
	mux.Handle("/", fake)
	mux.HandleFunc("GET /stand-in/messages", func(response http.ResponseWriter, _ *http.Request) {
		type listed struct {
			Account   string `json:"account"`
			MessageID string `json:"message_id"`
			Raw       string `json:"raw"`
		}
		items := []listed{}
		for _, sent := range fake.Messages() {
			items = append(items, listed{Account: sent.Account, MessageID: sent.MessageID, Raw: string(sent.Raw)})
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(items)
	})
	// Plays the person removing Colliedesk in their Microsoft account (My Apps).
	mux.HandleFunc("POST /stand-in/revoke-all", func(response http.ResponseWriter, _ *http.Request) {
		fake.RevokeAll()
		response.WriteHeader(http.StatusNoContent)
	})
	// Plays 90 days without a refresh: every refresh token has expired.
	mux.HandleFunc("POST /stand-in/expire", func(response http.ResponseWriter, _ *http.Request) {
		fake.ExpireAll()
		response.WriteHeader(http.StatusNoContent)
	})
	// Plays a work account whose Exchange Online licence is removed (?on=1) or given back (?on=0).
	mux.HandleFunc("POST /stand-in/no-mailbox", func(response http.ResponseWriter, request *http.Request) {
		on := request.URL.Query().Get("on") != "0"
		fake.Set(func(s *microsofttest.Server) { s.NoMailbox = on })
		response.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Microsoft stand-in on http://%s (client id %s)", *listen, *clientID)
	log.Fatal(server.ListenAndServe())
}
