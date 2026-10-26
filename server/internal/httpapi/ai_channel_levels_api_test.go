package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// levelDesk has two workspaces, each with a web form and a chat channel of a test type whose group
// scope declares intake, and a provider of its own.
type levelDesk struct {
	api                    http.Handler
	pool                   *pgxpool.Pool
	service                *auth.Service
	adminA, adminB         string
	workspaceA, workspaceB string
	providerA, providerA2  string
	formA, chatA, chatB    string
}

const levelChat channels.Type = "levelchat"

func newLevelDesk(t *testing.T) levelDesk {
	t.Helper()
	loop := channeltest.NewLoopback(levelChat, channels.Async)
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}}}
	loop.IntakeScopes = []string{"group"}
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(levelChat)); err != nil {
		t.Fatal(err)
	}
	fake := &aiprovider.Fake{}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.AI = fakeAIConfig(fake)
		config.ExtraChannelTypes = []channels.Adapter{loop}
	})
	d := levelDesk{api: api, pool: pool, service: service}
	d.adminA = createSession(t, api, service, "Level Desk A", "admin@level-a.example.com")
	d.adminB = createSession(t, api, service, "Level Desk B", "admin@level-b.example.com")
	d.workspaceA, d.workspaceB = workspaceID(t, api, d.adminA), workspaceID(t, api, d.adminB)
	d.providerA = create(t, api, d.adminA, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Team key", "api_key": "sk-level-0001", "models": []string{"claude-haiku-5-5"}})
	d.providerA2 = create(t, api, d.adminA, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Other key", "api_key": "sk-level-0002", "models": []string{"claude-haiku-5-5"}})
	providerB := create(t, api, d.adminB, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Team key", "api_key": "sk-level-0003", "models": []string{"claude-haiku-5-5"}})
	d.formA = d.channel(t, d.adminA, map[string]any{"type": "web_form", "config": map[string]any{"allowed_origins": []string{}}})
	d.chatA = d.channel(t, d.adminA, map[string]any{"type": string(levelChat), "config": map[string]any{"account": "level-a"}})
	d.chatB = d.channel(t, d.adminB, map[string]any{"type": string(levelChat), "config": map[string]any{"account": "level-b"}})
	for _, slot := range []string{"triage", "draft", "intake"} {
		d.setSlot(t, d.adminA, slot, map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true}, http.StatusOK)
		d.setSlot(t, d.adminB, slot, map[string]any{"source": "provider", "provider_id": providerB, "enabled": true}, http.StatusOK)
	}
	return d
}

func (d levelDesk) channel(t *testing.T, token string, body map[string]any) string {
	t.Helper()
	response := request(t, d.api, http.MethodPost, "/api/v1/channels", body, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create channel: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decode(t, response, &created)
	return created.ID
}

func (d levelDesk) setSlot(t *testing.T, token, slot string, body map[string]any, want int) ai.Slot {
	t.Helper()
	response := request(t, d.api, http.MethodPut, "/api/v1/ai/slots/"+slot, body, token, nil)
	if response.Code != want {
		t.Fatalf("set slot %s %v: %d %s", slot, body, response.Code, response.Body.String())
	}
	var result ai.Slot
	if want == http.StatusOK {
		decode(t, response, &result)
	}
	return result
}

func (d levelDesk) setLevel(t *testing.T, token, channel, slot string, body map[string]any) *httptestResponse {
	t.Helper()
	response := request(t, d.api, http.MethodPut, "/api/v1/channels/"+channel+"/ai/"+slot, body, token, nil)
	return &httptestResponse{Code: response.Code, Body: response.Body.String()}
}

type httptestResponse struct {
	Code int
	Body string
}

func (d levelDesk) levels(t *testing.T, token, channel string) ai.ChannelAI {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+channel+"/ai", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("channel ai: %d %s", response.Code, response.Body.String())
	}
	var levels ai.ChannelAI
	decode(t, response, &levels)
	return levels
}

func effective(levels ai.ChannelAI, scope, slot string) (ai.ChannelAISlot, bool) {
	for _, entry := range levels.Scopes {
		if entry.Scope != scope {
			continue
		}
		for _, item := range entry.Slots {
			if item.Slot == slot {
				return item, true
			}
		}
	}
	return ai.ChannelAISlot{}, false
}

// customerWritesOn starts a conversation on a channel's scope with one customer message and returns
// the kinds of automatic AI tasks it queued.
func (d levelDesk) customerWritesOn(t *testing.T, workspace, channel, scope string) []string {
	t.Helper()
	ctx := context.Background()
	var conversation string
	if err := d.pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ann','ann-'||gen_random_uuid()||'@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject,channel,channel_id,channel_scope)
		SELECT $1,c.id,'Printing fails',ch.type,ch.id,NULLIF($3,'') FROM c, channels ch WHERE ch.workspace_id=$1 AND ch.id=$2 RETURNING id::text`,
		workspace, channel, scope).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming','It fails')`, workspace, conversation); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	if err := d.pool.QueryRow(ctx, `SELECT COALESCE(array_agg(kind ORDER BY kind),'{}') FROM ai_tasks WHERE workspace_id=$1 AND conversation_id=$2 AND automatic`,
		workspace, conversation).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	return kinds
}

// A channel's scope reads its own level, then the channel's, then the workspace's default; a feature
// the workspace has off is off on every channel, and says why.
func TestAChannelScopeFollowsTheChannelThenTheWorkspace(t *testing.T) {
	d := newLevelDesk(t)
	levels := d.levels(t, d.adminA, d.chatA)
	if len(levels.Scopes) != 3 || levels.Scopes[0].Scope != "" || levels.Scopes[1].Scope != "user" || levels.Scopes[2].Scope != "group" {
		t.Fatalf("scopes = %+v", levels.Scopes)
	}
	if _, ok := effective(levels, "group", "intake"); !ok {
		t.Fatal("the group scope declares intake but has no intake row")
	}
	for _, scope := range []string{"", "user"} {
		if _, ok := effective(levels, scope, "intake"); ok {
			t.Fatalf("scope %q has intake", scope)
		}
	}
	if item, _ := effective(levels, "group", "triage"); item.EffectiveLevel != "suggest" || item.Source != "workspace" || item.Level != nil {
		t.Fatalf("group triage = %+v", item)
	}
	if response := d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "", "level": "off"}); response.Code != http.StatusOK {
		t.Fatalf("channel off: %+v", response)
	}
	if response := d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"}); response.Code != http.StatusOK {
		t.Fatalf("group auto: %+v", response)
	}
	levels = d.levels(t, d.adminA, d.chatA)
	for scope, want := range map[string][2]string{"": {"off", "channel"}, "user": {"off", "channel"}, "group": {"auto", "scope"}} {
		if item, _ := effective(levels, scope, "triage"); item.EffectiveLevel != want[0] || item.Source != want[1] {
			t.Errorf("scope %q triage = %+v, want %v", scope, item, want)
		}
	}
	// The scope's level decides what a customer message queues; one-to-one reads the channel's.
	if kinds := d.customerWritesOn(t, d.workspaceA, d.chatA, "group"); strings.Join(kinds, ",") != "triage" {
		t.Fatalf("group message queued %v", kinds)
	}
	if kinds := d.customerWritesOn(t, d.workspaceA, d.chatA, "user"); len(kinds) != 0 {
		t.Fatalf("one-to-one message queued %v", kinds)
	}
	// Turned off for the workspace, nothing runs anywhere and the channel says why.
	d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": false}, http.StatusOK)
	if item, _ := effective(d.levels(t, d.adminA, d.chatA), "group", "triage"); item.EffectiveLevel != "off" || item.OffReason == nil || *item.OffReason != "workspace_off" || item.Level == nil || *item.Level != "auto" {
		t.Fatalf("group triage with triage off = %+v", item)
	}
	if kinds := d.customerWritesOn(t, d.workspaceA, d.chatA, "group"); len(kinds) != 0 {
		t.Fatalf("group message queued %v with triage off", kinds)
	}
	// Settings → Built-in AI lists the levels channels set apart.
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	if len(settings.Channels) != 2 || settings.Channels[0].ChannelID != d.chatA {
		t.Fatalf("settings channels = %+v", settings.Channels)
	}
}

// Email and web forms have no intake: the route refuses it, says which features the scope has, and
// writes nothing; the same holds for a chat scope that does not declare it.
func TestIntakeLevelOnlyOnDeclaredScopes(t *testing.T) {
	d := newLevelDesk(t)
	for _, c := range []struct{ channel, scope string }{{d.formA, ""}, {d.chatA, "user"}, {d.chatA, ""}} {
		response := d.setLevel(t, d.adminA, c.channel, "intake", map[string]any{"scope": c.scope, "level": "suggest"})
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body, "intake_not_supported") || !strings.Contains(response.Body, `"available_slots":["triage","draft"]`) {
			t.Fatalf("intake on %s scope %q: %+v", c.channel, c.scope, response)
		}
	}
	if levels := d.levels(t, d.adminA, d.formA); len(levels.Scopes) != 1 || len(levels.Scopes[0].Slots) != 2 {
		t.Fatalf("web form levels = %+v", levels)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_ai_levels`); got != "0" {
		t.Fatalf("rows written: %s", got)
	}
	if response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "auto", "options": map[string]any{"attach_threshold": 0.92}}); response.Code != http.StatusOK {
		t.Fatalf("intake on the group scope: %+v", response)
	}
	item, _ := effective(d.levels(t, d.adminA, d.chatA), "group", "intake")
	if item.EffectiveLevel != "auto" || item.Options == nil || *item.Options.AttachThreshold != 0.92 || *item.Options.CreateThreshold != 0.85 || !*item.Options.AttachRequiresSingleCandidate {
		t.Fatalf("group intake = %+v options %+v", item, item.Options)
	}
	for _, refused := range []map[string]any{
		{"scope": "group", "level": "auto", "options": map[string]any{"attach_threshold": 0.2}},
		{"scope": "group", "level": "auto", "options": map[string]any{"guess": true}},
		{"scope": "group", "level": "auto", "options": map[string]any{"sources": map[string]any{"incoming": false}}},
		{"scope": "elsewhere", "level": "auto"},
	} {
		if response := d.setLevel(t, d.adminA, d.chatA, "intake", refused); response.Code != http.StatusBadRequest {
			t.Errorf("%v: %+v", refused, response)
		}
	}
}

// Drafts are never sent by AI and intake runs by itself only where a channel's scope says so.
func TestDraftsNeverReachAutoAndIntakeNotAsAWorkspaceDefault(t *testing.T) {
	d := newLevelDesk(t)
	if response := d.setLevel(t, d.adminA, d.formA, "draft", map[string]any{"scope": "", "level": "auto"}); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body, "ai_level_not_allowed") {
		t.Fatalf("draft auto on a channel: %+v", response)
	}
	for _, slot := range []string{"draft", "intake"} {
		response := request(t, d.api, http.MethodPut, "/api/v1/ai/slots/"+slot, map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true, "default_level": "auto"}, d.adminA, nil)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "ai_level_not_allowed") {
			t.Fatalf("%s default auto: %d %s", slot, response.Code, response.Body.String())
		}
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_slots WHERE default_level='auto'`); got != "0" {
		t.Fatalf("auto slots: %s", got)
	}
}

// A level that lets AI do more by itself waits for a person, whatever the token holds; lowering it is
// the admin tier's, and a lower tier's lowering waits too. Approving it applies it as the approver.
func TestARaiseFromAnAgentTokenWaitsForAPersonAndALowerDoesNot(t *testing.T) {
	d := newLevelDesk(t)
	admin := issueToken(t, d.api, d.adminA, "levels admin", "admin").Token
	draft := issueToken(t, d.api, d.adminA, "levels draft", "draft").Token
	raise := map[string]any{"scope": "group", "level": "auto"}
	response := d.setLevel(t, admin, d.chatA, "intake", raise)
	if response.Code != http.StatusAccepted {
		t.Fatalf("admin token raises intake: %+v", response)
	}
	var filed struct {
		RequestID string `json:"request_id"`
		Summary   string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(response.Body), &filed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filed.Summary, "intake") || !strings.Contains(filed.Summary, "group") || !strings.Contains(filed.Summary, "→ auto") {
		t.Fatalf("summary = %q", filed.Summary)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_ai_levels`); got != "0" {
		t.Fatalf("a filed raise wrote %s rows", got)
	}
	if approved := approve(t, d.api, d.adminA, filed.RequestID, nil); approved.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	if item, _ := effective(d.levels(t, d.adminA, d.chatA), "group", "intake"); item.EffectiveLevel != "auto" {
		t.Fatalf("after approval: %+v", item)
	}
	// Lowering runs at once for the admin tier; a draft token's waits.
	if response := d.setLevel(t, draft, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest"}); response.Code != http.StatusAccepted {
		t.Fatalf("draft token lowers: %+v", response)
	}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest"}); response.Code != http.StatusOK {
		t.Fatalf("admin token lowers: %+v", response)
	}
	// Removing a scope's own level raises when what it then follows is higher.
	d.setLevel(t, d.adminA, d.formA, "triage", map[string]any{"scope": "", "level": "off"})
	if response := d.setLevel(t, admin, d.formA, "triage", map[string]any{"scope": "", "level": nil}); response.Code != http.StatusAccepted {
		t.Fatalf("follow a higher workspace default: %+v", response)
	}
	// Loosening intake's options is a raise; tightening is not.
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": map[string]any{"attach_threshold": 0.95}}); response.Code != http.StatusOK {
		t.Fatalf("tighten: %+v", response)
	}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": map[string]any{"attach_threshold": 0.8}}); response.Code != http.StatusAccepted {
		t.Fatalf("loosen: %+v", response)
	}
	// Masking more is a tightening; masking less a raise; a label is a word, not a sentence.
	labels := map[string]any{"attach_threshold": 0.95, "contact_labels": map[string]any{"account": []string{"stk", "số tài khoản"}, "id": []string{"cccd"}}}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": labels}); response.Code != http.StatusOK {
		t.Fatalf("mask more: %+v", response)
	}
	fewer := map[string]any{"attach_threshold": 0.95, "contact_labels": map[string]any{"account": []string{"stk"}, "id": []string{"cccd"}}}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": fewer}); response.Code != http.StatusAccepted {
		t.Fatalf("mask less: %+v", response)
	}
	// A channel's own patterns: adding one tightens, taking one out is a raise, a pattern that would mask
	// everywhere, does not compile or is too large is refused.
	patterns := []map[string]any{{"pattern": `\b\d{12}\b`, "mask": "id_number"}, {"pattern": `\b84\d{9}\b`, "mask": "phone"}}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest",
		"options": map[string]any{"attach_threshold": 0.95, "contact_labels": labels["contact_labels"], "redact_patterns": patterns}}); response.Code != http.StatusOK {
		t.Fatalf("add patterns: %+v", response)
	}
	if response := d.setLevel(t, admin, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest",
		"options": map[string]any{"attach_threshold": 0.95, "contact_labels": labels["contact_labels"], "redact_patterns": patterns[:1]}}); response.Code != http.StatusAccepted {
		t.Fatalf("take a pattern out: %+v", response)
	}
	for _, pattern := range []string{`\d*`, `\b`, `\b\d*`, `(?:\b)`, `\d*\b`, `(`, `[a-z]{999}[a-z]{999}[a-z]{999}`, strings.Repeat("a", 201)} {
		body := map[string]any{"scope": "group", "level": "suggest", "options": map[string]any{"redact_patterns": []map[string]any{{"pattern": pattern, "mask": "phone"}}}}
		if response := d.setLevel(t, d.adminA, d.chatA, "intake", body); response.Code != http.StatusBadRequest {
			t.Fatalf("pattern %q: %+v", pattern[:min(len(pattern), 20)], response)
		}
	}
	// The settings page shows a refusal as it comes: it names the setting as the page does, and which pattern.
	for _, tc := range []struct {
		options map[string]any
		says    string
	}{
		{map[string]any{"create_threshold": 0.3}, "threshold to open a conversation"},
		{map[string]any{"redact_patterns": []map[string]any{{"pattern": `\b\d{12}\b`, "mask": "id_number"}, {"pattern": `(`, "mask": "phone"}}}, "Pattern 2 "},
		{map[string]any{"redact_patterns": []map[string]any{{"pattern": `\d*`, "mask": "phone"}}}, "Pattern 1 "},
		{map[string]any{"redact_patterns": []map[string]any{{"pattern": `\d{12}`, "mask": "name"}}}, "Pattern 1 "},
	} {
		response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": tc.options})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, tc.says) ||
			strings.Contains(response.Body, "redact_patterns") || strings.Contains(response.Body, "_threshold") {
			t.Fatalf("refusal of %v = %+v, want it to say %q without a field name", tc.options, response, tc.says)
		}
	}
	bad := map[string]any{"contact_labels": map[string]any{"account": []string{" stk "}}}
	if response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest", "options": bad}); response.Code != http.StatusBadRequest {
		t.Fatalf("a label with spaces at its ends: %+v", response)
	}
	// A person who is not an admin cannot change a level.
	agent := joinAsAgent(t, d.api, d.adminA, "agent@level-a.example.com")
	if response := d.setLevel(t, agent, d.chatA, "triage", map[string]any{"scope": "", "level": "off"}); response.Code != http.StatusForbidden {
		t.Fatalf("an agent sets a level: %+v", response)
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.chatA+"/ai", nil, agent, nil); response.Code != http.StatusOK {
		t.Fatalf("an agent reads the levels: %d", response.Code)
	}
}

// A channel's own settings never become another channel's: every channel of a type reads the type's
// defaults, and a chat type's defaults hold maps (its commands) that one channel's settings must not write
// into. A card of workspace B lists the commands of its own chat only. (A web form's default tags are a
// new list each time the type is read, so they are no such path.)
func TestAChannelsSettingsNeverReachAnotherWorkspacesChannel(t *testing.T) {
	d := newLevelDesk(t)
	ctx := context.Background()
	if response := request(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.chatA, map[string]any{"settings": json.RawMessage(
		`{"default_tags":["alpha-vip"],"commands":{"vip":{"phrases":["alpha vip please"],"tags":["alpha-vip"]}}}`)}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("set chat A's settings: %d %s", response.Code, response.Body.String())
	}
	cardOf := func(workspace, channel string) string {
		var id string
		if err := d.pool.QueryRow(ctx, `WITH th AS (INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,opens_conversations)
				VALUES($1,$2,'group','group:1',true) RETURNING id)
			INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,state,review_reason,rules_action,rules_rule,opened_at,closed_at)
			SELECT $1,$2,'group','thread:'||th.id||':u',th.id,'review','no_command','create','new_thread',now(),now() FROM th RETURNING id::text`,
			workspace, channel).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, card := range []struct{ token, id, want string }{
		{d.adminB, cardOf(d.workspaceB, d.chatB), `["bug"]`},
		{d.adminA, cardOf(d.workspaceA, d.chatA), `["bug","vip"]`},
	} {
		response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings/"+card.id, nil, card.token, nil)
		var detail struct {
			Commands json.RawMessage `json:"commands"`
		}
		decode(t, response, &detail)
		if string(detail.Commands) != card.want {
			t.Fatalf("commands of card %s = %s, want %s", card.id, detail.Commands, card.want)
		}
	}
}

// Moving a feature to another provider takes back what channels let it do by itself (the consent was
// for another recipient), in the same change, with the reason in both histories.
func TestChangingTheRecipientLowersEveryChannelThatRanByItself(t *testing.T) {
	d := newLevelDesk(t)
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"})
	d.setLevel(t, d.adminA, d.chatA, "draft", map[string]any{"scope": "group", "level": "suggest"})
	d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "auto"})
	d.setLevel(t, d.adminA, d.formA, "triage", map[string]any{"scope": "", "level": "off"})
	for _, slot := range []string{"triage", "draft", "intake"} {
		d.setSlot(t, d.adminA, slot, map[string]any{"source": "provider", "provider_id": d.providerA2, "enabled": true}, http.StatusOK)
	}
	levels := d.levels(t, d.adminA, d.chatA)
	for slot, want := range map[string]string{"triage": "suggest", "draft": "off", "intake": "suggest"} {
		if item, _ := effective(levels, "group", slot); item.Level == nil || *item.Level != want {
			t.Errorf("group %s after the recipient changed = %+v, want %s", slot, item, want)
		}
	}
	if item, _ := effective(d.levels(t, d.adminA, d.formA), "", "triage"); item.Level == nil || *item.Level != "off" {
		t.Fatalf("an off level changed: %+v", item)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_settings_events WHERE workspace_id=$1 AND kind='channel_level_changed' AND detail->>'reason'='recipient_changed'`, d.workspaceA); got != "3" {
		t.Fatalf("recipient_changed events = %s", got)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_events WHERE workspace_id=$1 AND kind='ai_changed' AND detail->>'reason'='recipient_changed'`, d.workspaceA); got != "3" {
		t.Fatalf("channel ai_changed events = %s", got)
	}
	// Deleting the provider turns the features off; the channel rows stay, and run nothing.
	if response := request(t, d.api, http.MethodDelete, "/api/v1/ai/providers/"+d.providerA2, nil, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("remove provider: %d %s", response.Code, response.Body.String())
	}
	for _, entry := range d.levels(t, d.adminA, d.chatA).Scopes {
		for _, item := range entry.Slots {
			if item.EffectiveLevel != "off" || item.OffReason == nil {
				t.Errorf("scope %q %s after the provider went = %+v", entry.Scope, item.Slot, item)
			}
		}
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_slots WHERE workspace_id=$1 AND (enabled OR default_level<>'off')`, d.workspaceA); got != "0" {
		t.Fatalf("slots still on: %s", got)
	}
}

// An older client sends enabled and automatic; they map onto the levels and a customer message queues
// what it did before.
func TestAnOlderClientsSwitchesKeepWhatTheyMeant(t *testing.T) {
	d := newLevelDesk(t)
	draft := d.setSlot(t, d.adminA, "draft", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true}, http.StatusOK)
	triage := d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true}, http.StatusOK)
	if draft.DefaultLevel != "off" || draft.Automatic || triage.DefaultLevel != "suggest" || triage.Automatic {
		t.Fatalf("turned on: draft %+v triage %+v", draft, triage)
	}
	if kinds := d.customerWritesOn(t, d.workspaceA, d.formA, ""); strings.Join(kinds, ",") != "triage" {
		t.Fatalf("triage on, drafts on demand: %v", kinds)
	}
	draft = d.setSlot(t, d.adminA, "draft", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true, "automatic": true}, http.StatusOK)
	triage = d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true, "automatic": true}, http.StatusOK)
	if draft.DefaultLevel != "suggest" || !draft.Automatic || triage.DefaultLevel != "auto" || !triage.Automatic || len(triage.AutoParts) != 2 {
		t.Fatalf("automatic: draft %+v triage %+v", draft, triage)
	}
	if kinds := d.customerWritesOn(t, d.workspaceA, d.formA, ""); strings.Join(kinds, ",") != "draft,triage" {
		t.Fatalf("both automatic: %v", kinds)
	}
	// Leaving automatic out keeps the level; turning it off goes back a step.
	if kept := d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true, "effort": "medium"}, http.StatusOK); kept.DefaultLevel != "auto" {
		t.Fatalf("kept: %+v", kept)
	}
	if off := d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": true, "automatic": false}, http.StatusOK); off.DefaultLevel != "suggest" {
		t.Fatalf("automatic false: %+v", off)
	}
	if off := d.setSlot(t, d.adminA, "draft", map[string]any{"source": "provider", "provider_id": d.providerA, "enabled": false}, http.StatusOK); off.DefaultLevel != "off" {
		t.Fatalf("drafts off: %+v", off)
	}
}

// Another workspace's channel is not there, and nothing of one workspace's levels or consents shows
// in another's.
func TestChannelLevelsAndSourceConsentsStayInTheirWorkspace(t *testing.T) {
	d := newLevelDesk(t)
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"})
	if response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.chatA+"/ai", nil, d.adminB, nil); response.Code != http.StatusNotFound {
		t.Fatalf("B reads A's channel: %d %s", response.Code, response.Body.String())
	}
	if response := d.setLevel(t, d.adminB, d.chatA, "triage", map[string]any{"scope": "", "level": "off"}); response.Code != http.StatusNotFound {
		t.Fatalf("B sets A's channel: %+v", response)
	}
	tokenB := issueToken(t, d.api, d.adminB, "levels b", "admin").Token
	if response := d.setLevel(t, tokenB, d.chatA, "triage", map[string]any{"scope": "", "level": "auto"}); response.Code != http.StatusNotFound {
		t.Fatalf("B's token files a raise on A's channel: %+v", response)
	}
	if response := request(t, d.api, http.MethodPut, "/api/v1/ai/source-consents/images", nil, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("consent: %d %s", response.Code, response.Body.String())
	}
	var settingsB ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminB, nil), &settingsB)
	if len(settingsB.Channels) != 0 || len(settingsB.SourceConsents) != 0 {
		t.Fatalf("B sees %+v %+v", settingsB.Channels, settingsB.SourceConsents)
	}
	if item, _ := effective(d.levels(t, d.adminB, d.chatB), "group", "triage"); item.Source != "workspace" {
		t.Fatalf("B's channel = %+v", item)
	}
}

// A source consent is data leaving, so a token asks for it; taking it back is the admin tier's.
// Both are in the AI history.
func TestASourceConsentWaitsForAPersonAndIsTakenBackAtOnce(t *testing.T) {
	d := newLevelDesk(t)
	admin := issueToken(t, d.api, d.adminA, "consent admin", "admin").Token
	if response := request(t, d.api, http.MethodPut, "/api/v1/ai/source-consents/images", nil, admin, nil); response.Code != http.StatusAccepted {
		t.Fatalf("token consent: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodPut, "/api/v1/ai/source-consents/passwords", nil, d.adminA, nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown class: %d", response.Code)
	}
	for range 2 {
		if response := request(t, d.api, http.MethodPut, "/api/v1/ai/source-consents/contact_profile", nil, d.adminA, nil); response.Code != http.StatusOK {
			t.Fatalf("consent: %d %s", response.Code, response.Body.String())
		}
	}
	var removed struct {
		Removed bool `json:"removed"`
	}
	decode(t, request(t, d.api, http.MethodDelete, "/api/v1/ai/source-consents/contact_profile", nil, admin, nil), &removed)
	if !removed.Removed {
		t.Fatal("not removed")
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_settings_events WHERE workspace_id=$1 AND kind='source_consent_changed'`, d.workspaceA); got != "2" {
		t.Fatalf("consent events = %s", got)
	}
}

// Every change of a level is in the AI history and in the channel's own history.
func TestALevelChangeIsInBothHistories(t *testing.T) {
	d := newLevelDesk(t)
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"})
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"}) // the same again changes nothing
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": nil})
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	var changes []map[string]any
	for _, event := range settings.Events {
		if event.Kind == "channel_level_changed" {
			var detail map[string]any
			_ = json.Unmarshal(event.Detail, &detail)
			changes = append(changes, detail)
		}
	}
	if len(changes) != 2 || changes[0]["before"] != "auto" || changes[0]["after"] != nil || changes[1]["after"] != "auto" || changes[1]["scope"] != "group" {
		t.Fatalf("AI history = %+v", changes)
	}
	var page struct {
		Items []struct {
			Kind   string   `json:"kind"`
			Fields []string `json:"fields"`
		} `json:"items"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/events?channel_id="+d.chatA, nil, d.adminA, nil), &page)
	count := 0
	for _, item := range page.Items {
		if item.Kind == "ai_changed" && len(item.Fields) == 1 && item.Fields[0] == "triage" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("channel history = %+v", page.Items)
	}
}

// A call that could never run is refused at once, also from a token that would only file it: nobody
// approves a request for a scope the channel lacks or intake on a web form.
func TestALevelThatCannotRunIsRefusedBeforeItIsFiled(t *testing.T) {
	d := newLevelDesk(t)
	draft := issueToken(t, d.api, d.adminA, "levels draft", "draft").Token
	if response := d.setLevel(t, draft, d.chatA, "triage", map[string]any{"scope": "nonsense", "level": "off"}); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown scope: %+v", response)
	}
	if response := d.setLevel(t, draft, d.formA, "intake", map[string]any{"scope": "", "level": "suggest"}); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body, "intake_not_supported") {
		t.Fatalf("intake on a web form: %+v", response)
	}
	if response := d.setLevel(t, draft, d.formA, "draft", map[string]any{"scope": "", "level": "auto"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("draft auto: %+v", response)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM agent_requests WHERE workspace_id=$1`, d.workspaceA); got != "0" {
		t.Fatalf("filed %s requests", got)
	}
	// Options within every field's limit can still pass the 16 KiB a scope keeps; that is said plainly.
	keywords := make([]string, 100)
	for i := range keywords {
		keywords[i] = strings.Repeat("ệ", 79) + string(rune('a'+i%26))
	}
	response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest",
		"options": map[string]any{"instructions": strings.Repeat("ệ", 4000), "redact_keywords": keywords}})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, "16 KiB") {
		t.Fatalf("options over 16 KiB: %+v", response)
	}
	cjk := make([]string, 30)
	for i := range cjk {
		cjk[i] = strings.Repeat("密", 79) + string(rune('a'+i))
	}
	response = d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest",
		"options": map[string]any{"instructions": strings.Repeat("密", 4000), "redact_keywords": cjk}})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, "16 KiB") {
		t.Fatalf("CJK options over 16 KiB: %+v", response)
	}
}

// Intake's gate is passed with one model's decisions, so another model of the same provider takes its
// channels back to suggest; triage and drafts keep their level on another model, as before levels, and
// so does every feature when only the effort changes.
func TestAnotherModelLowersIntakeOnly(t *testing.T) {
	d := newLevelDesk(t)
	two := create(t, d.api, d.adminA, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Two models", "api_key": "sk-level-0004",
		"models": []string{"claude-haiku-5-5", "claude-sonnet-5-5"}})
	for _, slot := range []string{"triage", "intake"} {
		d.setSlot(t, d.adminA, slot, map[string]any{"source": "provider", "provider_id": two, "model": "claude-haiku-5-5", "enabled": true}, http.StatusOK)
	}
	d.setLevel(t, d.adminA, d.chatA, "triage", map[string]any{"scope": "group", "level": "auto"})
	d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "auto"})
	for _, slot := range []string{"triage", "intake"} {
		d.setSlot(t, d.adminA, slot, map[string]any{"source": "provider", "provider_id": two, "model": "claude-haiku-5-5", "effort": "medium", "enabled": true}, http.StatusOK)
	}
	levels := d.levels(t, d.adminA, d.chatA)
	for _, slot := range []string{"triage", "intake"} {
		if item, _ := effective(levels, "group", slot); item.EffectiveLevel != "auto" {
			t.Fatalf("%s after another effort = %+v", slot, item)
		}
	}
	for _, slot := range []string{"triage", "intake"} {
		d.setSlot(t, d.adminA, slot, map[string]any{"source": "provider", "provider_id": two, "model": "claude-sonnet-5-5", "enabled": true}, http.StatusOK)
	}
	levels = d.levels(t, d.adminA, d.chatA)
	if item, _ := effective(levels, "group", "triage"); item.EffectiveLevel != "auto" {
		t.Fatalf("triage after another model = %+v", item)
	}
	if item, _ := effective(levels, "group", "intake"); item.Level == nil || *item.Level != "suggest" {
		t.Fatalf("intake after another model = %+v", item)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_settings_events WHERE workspace_id=$1 AND kind='channel_level_changed' AND detail->>'reason'='model_changed'`, d.workspaceA); got != "1" {
		t.Fatalf("model_changed events = %s", got)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_events WHERE workspace_id=$1 AND kind='ai_changed' AND detail->>'reason'='model_changed'`, d.workspaceA); got != "1" {
		t.Fatalf("channel model_changed events = %s", got)
	}
	// The channel says when and why the server lowered it, until a person changes the level again.
	if item, _ := effective(levels, "group", "intake"); item.Lowered == nil || item.Lowered.Reason != "model_changed" {
		t.Fatalf("intake lowered = %+v", item.Lowered)
	}
	if item, _ := effective(levels, "group", "triage"); item.Lowered != nil {
		t.Fatalf("triage lowered = %+v", item.Lowered)
	}
	if response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "off"}); response.Code != http.StatusOK {
		t.Fatalf("a person turns intake off: %+v", response)
	}
	if item, _ := effective(d.levels(t, d.adminA, d.chatA), "group", "intake"); item.Lowered != nil {
		t.Fatalf("lowered after a person's change = %+v", item.Lowered)
	}
}

// Right at 16 KiB, options are either kept or refused with the limit named, never refused by the
// database with a message that says nothing: the check measures them as the database writes jsonb.
func TestIntakeOptionsAtTheLimitAreKeptOrRefusedPlainly(t *testing.T) {
	d := newLevelDesk(t)
	keywords := make([]string, 100)
	for i := range keywords {
		keywords[i] = strings.Repeat("漢", 23) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	kept, refused := 0, 0
	for n := 2880; n <= 2990; n++ {
		response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "suggest",
			"options": map[string]any{"instructions": strings.Repeat("漢", n), "redact_keywords": keywords}})
		switch {
		case response.Code == http.StatusOK:
			kept++
		case response.Code == http.StatusBadRequest && strings.Contains(response.Body, "16 KiB"):
			refused++
		default:
			t.Fatalf("instructions of %d characters: %+v", n, response)
		}
	}
	if kept == 0 || refused == 0 {
		t.Fatalf("the sweep missed the limit: kept %d, refused %d", kept, refused)
	}
}
