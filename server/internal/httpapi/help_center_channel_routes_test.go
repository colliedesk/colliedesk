package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// loopHelpType is a channel type no server ships, with a help center route (channeltest.Loopback).
const loopHelpType channels.Type = "loophelp"

// loopHelpChannel is a loopback channel with a way to turn it off and on.
type loopHelpChannel struct {
	id     string
	enable func(on bool)
}

func loopHelpAPI(t *testing.T, mutate func(*httpapi.Config)) (http.Handler, *auth.Service, *fakeDNS, func(admin string) loopHelpChannel) {
	t.Helper()
	api, service, pool, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.ExtraChannelTypes = []channels.Adapter{channeltest.NewLoopback(loopHelpType, channels.Sync)}
		if mutate != nil {
			mutate(config)
		}
	})
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(loopHelpType)); err != nil {
		t.Fatal(err)
	}
	loop := channeltest.NewLoopback(loopHelpType, channels.Sync)
	registry, err := channels.NewRegistry(loop)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(pool).WithChannels(registry)
	connect := func(admin string) loopHelpChannel {
		var workspaceID, agentID string
		me := kbCall(t, api, http.MethodGet, "/api/v1/auth/me", nil, admin, http.StatusOK, nil)
		var who struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Workspace struct {
				ID string `json:"id"`
			} `json:"workspace"`
		}
		decode(t, me, &who)
		workspaceID, agentID = who.Workspace.ID, who.Agent.ID
		channel, err := st.CreateChannel(context.Background(), workspaceID, loopHelpType, store.ChannelInput{Config: json.RawMessage(`{"account":"acct-` + agentID + `"}`)},
			store.ChannelActor{AgentID: agentID})
		if err != nil {
			t.Fatal(err)
		}
		return loopHelpChannel{id: channel.ID, enable: func(on bool) {
			if _, err := st.UpdateChannel(context.Background(), workspaceID, channel.ID, store.ChannelInput{Enabled: &on}, store.ChannelActor{AgentID: agentID}); err != nil {
				t.Fatal(err)
			}
		}}
	}
	return api, service, dns, connect
}

func loopHelpAnswer(t *testing.T, response *httptest.ResponseRecorder) channeltest.LoopbackHelpCenterAnswer {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var answer channeltest.LoopbackHelpCenterAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatalf("%v: %s", err, response.Body.String())
	}
	return answer
}

func helpCenterCall(site http.Handler, method, host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr, req.Host = "192.0.2.81:4242", host
	return serve(site, req)
}

// A channel type's help center routes answer below every address of an open help center, for that
// workspace's channels only; a help center that is off, a channel of another workspace and a channel
// that is off get the very answer of a help center that is not there.
func TestChannelTypeRoutesServeOnTheHelpCenterPath(t *testing.T) {
	api, service, _, connect := loopHelpAPI(t, nil)
	site := helpCenterSite(api)
	adminA := createSession(t, api, service, "Loop Help A", "admin@loop-help-a.example.com")
	adminB := createSession(t, api, service, "Loop Help B", "admin@loop-help-b.example.com")
	slugA, slugB := workspaceSlug(t, api, adminA), workspaceSlug(t, api, adminB)
	enableHelpCenter(t, api, adminA, "Loop A", true)
	enableHelpCenter(t, api, adminB, "Loop B", true)
	loopA, loopB := connect(adminA), connect(adminB)
	channelA, channelB := loopA.id, loopB.id

	answer := loopHelpAnswer(t, helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slugA+"/loophelp"))
	if answer.ChannelID != channelA || answer.Base != "/help/"+slugA || answer.Address != testOrigin+"/help/"+slugA {
		t.Fatalf("GET answer = %+v", answer)
	}
	if answer := loopHelpAnswer(t, helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slugA+"/loophelp/hello?channel="+channelA)); answer.Name != "hello" ||
		answer.ChannelID != channelA {
		t.Fatalf("POST answer = %+v", answer)
	}

	missing := helpCenterCall(site, http.MethodGet, testOriginHost, "/help/no-such-help-center/loophelp")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown help center status=%d", missing.Code)
	}
	same := func(name string, response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != missing.Code || response.Body.String() != missing.Body.String() ||
			response.Header().Get("Cache-Control") != missing.Header().Get("Cache-Control") {
			t.Errorf("%s: status=%d, want the 404 of a help center that is not there", name, response.Code)
		}
	}
	same("another workspace's channel", helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slugA+"/loophelp?channel="+channelB))
	same("another workspace's channel by POST", helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slugB+"/loophelp/x?channel="+channelA))
	same("a channel id that is no id", helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slugA+"/loophelp?channel=x"))

	// A route never shadows the help center's own pages, and an unknown write is still refused.
	if response := helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slugA+"/search"); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /search status=%d", response.Code)
	}
	if response := helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slugA+"/loophelp"); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST to a GET route status=%d", response.Code)
	}

	loopA.enable(false)
	same("a channel that is off", helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slugA+"/loophelp"))
	loopA.enable(true)
	enableHelpCenter(t, api, adminA, "Loop A", false)
	same("a help center that is off", helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slugA+"/loophelp"))
	same("a help center that is off, by POST", helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slugA+"/loophelp/x"))
}

// With a help center domain the routes answer on <slug>.<domain> and on an active custom domain; the
// path address moves a page there but never turns a write into a GET.
func TestChannelTypeRoutesServeOnTheHelpCenterHostAndCustomDomain(t *testing.T) {
	api, service, dns, connect := loopHelpAPI(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Loop Host", "admin@loop-host.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Loop Host", true)
	channel := connect(admin).id

	host := slug + ".help.example.net"
	answer := loopHelpAnswer(t, helpCenterCall(site, http.MethodGet, host, "/loophelp"))
	if answer.ChannelID != channel || answer.Base != "" || answer.Address != "https://"+host {
		t.Fatalf("host answer = %+v", answer)
	}
	if response := helpCenterCall(site, http.MethodGet, testOriginHost, "/help/"+slug+"/loophelp"); response.Code != http.StatusMovedPermanently ||
		response.Header().Get("Location") != "https://"+host+"/loophelp" {
		t.Errorf("path GET status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	if response := helpCenterCall(site, http.MethodPost, testOriginHost, "/help/"+slug+"/loophelp/x"); response.Code != http.StatusNotFound {
		t.Errorf("path POST status=%d, want 404", response.Code)
	}

	const custom = "help.loop-host.example"
	view := setDomain(t, api, admin, custom, http.StatusOK)
	dns.set("_help-center-verify."+custom, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	answer = loopHelpAnswer(t, helpCenterCall(site, http.MethodPost, custom, "/loophelp/on-custom"))
	if answer.ChannelID != channel || answer.Base != "" || answer.Address != "https://"+custom || answer.Name != "on-custom" {
		t.Fatalf("custom domain answer = %+v", answer)
	}
	// The help center's own host now moves pages to the custom domain; a write there is not moved.
	if response := helpCenterCall(site, http.MethodGet, host, "/loophelp"); response.Code != http.StatusMovedPermanently {
		t.Errorf("old host GET status=%d", response.Code)
	}
	if response := helpCenterCall(site, http.MethodPost, host, "/loophelp/x"); response.Code != http.StatusNotFound {
		t.Errorf("old host POST status=%d, want 404", response.Code)
	}
}
