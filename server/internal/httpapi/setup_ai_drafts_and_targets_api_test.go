package httpapi_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The setup's AI drafts and reply targets steps (docs/user/setup-guide.md): where each stands, and the reply
// targets preset the team's own history suggests.

type replyTargetsSuggestionView struct {
	Preset                  string `json:"preset"`
	Basis                   string `json:"basis"`
	MedianFirstReplyMinutes *int   `json:"median_first_reply_minutes"`
}

type replyTargetPresetView struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Targets map[string]struct {
		FirstResponse int `json:"first_response_minutes"`
		Resolution    int `json:"resolution_minutes"`
	} `json:"targets"`
}

type setupTargetsView struct {
	Steps []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"steps"`
	Presets    []replyTargetPresetView    `json:"reply_target_presets"`
	Suggestion replyTargetsSuggestionView `json:"reply_target_suggestion"`
}

func readSetupTargets(t *testing.T, api http.Handler, token string) setupTargetsView {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/v1/workspace/setup", nil, token, nil)
	response := serve(api, req)
	if response.Code != http.StatusOK {
		t.Fatalf("GET setup status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, req, response)
	var view setupTargetsView
	decode(t, response, &view)
	return view
}

// serverDrafts is a server whose operator offers built-in AI for reply drafts.
func serverDrafts(config *httpapi.Config) {
	config.AI.Server = &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-secret",
		Models: map[string][]string{ai.SlotDraft: {"claude-sonnet-5-5"}}}
}

func wantStep(t *testing.T, view setupView, key, want string) {
	t.Helper()
	if got := view.state(t, key); got != want {
		t.Fatalf("%s = %q, want %q (steps %+v)", key, got, want, view.Steps)
	}
}

func publishArticle(t *testing.T, pool *pgxpool.Pool, workspace string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `WITH col AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Help','help') RETURNING id)
		INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
		SELECT $1,col.id,1,'Refunds','refunds','How refunds work.','published',now() FROM col`, workspace); err != nil {
		t.Fatal(err)
	}
}

func TestSetupHasAIDraftsAndReplyTargetsBetweenTheImportAndTheInvites(t *testing.T) {
	desk := newAIDesk(t, serverDrafts)
	view := readSetupTargets(t, desk.api, desk.adminA)
	var keys, names []string
	for _, step := range view.Steps {
		keys, names = append(keys, step.Key), append(names, step.Name)
	}
	if want := []string{"email", "import", "ai_drafts", "reply_targets", "invite", "first_reply"}; !slices.Equal(keys, want) {
		t.Fatalf("order = %v, want %v", keys, want)
	}
	if want := []string{"Receive customer email", "Import from Freshdesk", "Turn on AI drafts", "Set reply targets", "Invite your team", "Answer your first email"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestSetupAIDraftsStepFollowsTheDraftsFeatureAndWhatItCanRead(t *testing.T) {
	desk := newAIDesk(t, serverDrafts)
	slug := workspaceSlug(t, desk.api, desk.adminA)
	// Nothing imported and no article: drafts would have only the conversation to read.
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "nothing_to_read")
	insertImport(t, desk.pool, slug, "running")
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "nothing_to_read")
	insertImport(t, desk.pool, slug, "finished")
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "none")

	// Drafts only on request (Draft with AI) is not the step: a draft must wait in each new conversation.
	set := func(body map[string]any) {
		t.Helper()
		if response := request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", body, desk.adminA, nil); response.Code != http.StatusOK {
			t.Fatalf("PUT draft status=%d body=%s", response.Code, response.Body.String())
		}
	}
	set(map[string]any{"source": "server", "enabled": true, "default_level": "off"})
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "none")
	set(map[string]any{"source": "server", "enabled": true, "default_level": "suggest"})
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "on")
	set(map[string]any{"source": "server", "enabled": false})
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "none")

	// A published article is something to read too; workspace B has neither and stays as it is.
	publishArticle(t, desk.pool, workspaceID(t, desk.api, desk.adminB))
	wantStep(t, readSetup(t, desk.api, desk.adminB), "ai_drafts", "none")
}

func TestSetupAIDraftsStepIsUnavailableWithoutBuiltInAIOrAProvider(t *testing.T) {
	// No built-in AI on this server: workspace A has its own provider, a new workspace has none.
	desk := newAIDesk(t, nil)
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.AI = fakeAIConfig(&aiprovider.Fake{}) })
	fresh := createSession(t, api, service, "No AI Desk", "admin@no-ai.example.com")
	wantStep(t, readSetup(t, api, fresh), "ai_drafts", "unavailable")
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "nothing_to_read")
	set := request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true, "default_level": "suggest"}, desk.adminA, nil)
	if set.Code != http.StatusOK {
		t.Fatalf("PUT draft status=%d body=%s", set.Code, set.Body.String())
	}
	wantStep(t, readSetup(t, desk.api, desk.adminA), "ai_drafts", "on")
}

func TestSetupReplyTargetsStepIsOnOnceTargetsAreOn(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Targets Desk", "admin@targets.example.com")
	other := createSession(t, api, service, "Other Targets Desk", "admin@other-targets.example.com")
	wantStep(t, readSetup(t, api, admin), "reply_targets", "none")
	put := func(enabled bool) {
		t.Helper()
		body := map[string]any{"enabled": enabled, "warn_before_minutes": 30,
			"hours":   []any{map[string]any{"day": "mon", "open": true, "from": "09:00", "to": "18:00"}},
			"targets": map[string]any{"normal": map[string]any{"first_response_minutes": 480, "resolution_minutes": 1620}}}
		if response := request(t, api, http.MethodPut, "/api/v1/response-targets", body, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("PUT targets status=%d body=%s", response.Code, response.Body.String())
		}
	}
	put(true)
	wantStep(t, readSetup(t, api, admin), "reply_targets", "on")
	wantStep(t, readSetup(t, api, other), "reply_targets", "none")
	put(false)
	wantStep(t, readSetup(t, api, admin), "reply_targets", "none")
}

func TestSetupSuggestsTheReplyTargetsPresetFromTheImportedHistory(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "History Desk", "admin@history.example.com")
	other := createSession(t, api, service, "Other History Desk", "admin@other-history.example.com")
	workspace, slug := workspaceID(t, api, admin), workspaceSlug(t, api, admin)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Imported Person", "email": "imported@customer.example"})
	// imported writes a conversation from the import, opened ago, answered after reply (none: never answered).
	imported := func(ago time.Duration, reply *time.Duration) {
		t.Helper()
		opened := time.Now().UTC().Add(-ago)
		conversation := queryString(t, pool, `INSERT INTO conversations(workspace_id,contact_id,subject,status,origin,channel,created_at,updated_at)
			VALUES($1,$2,'Imported','resolved','{"source":"freshdesk"}','imported',$3,$3) RETURNING id::text`, workspace, contact, opened)
		write := func(kind string, at time.Time) {
			if _, err := pool.Exec(t.Context(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin,created_at) VALUES($1,$2,$3,'Text','{"source":"freshdesk"}',$4)`,
				workspace, conversation, kind, at); err != nil {
				t.Fatal(err)
			}
		}
		write("incoming", opened)
		write("note", opened.Add(time.Minute))
		if reply != nil {
			write("outgoing", opened.Add(*reply))
			write("outgoing", opened.Add(*reply+time.Hour))
		}
	}
	minutes := func(n int) *time.Duration { d := time.Duration(n) * time.Minute; return &d }
	presetOf := func(token string) replyTargetsSuggestionView {
		t.Helper()
		return readSetupTargets(t, api, token).Suggestion
	}
	want := func(got replyTargetsSuggestionView, preset, basis string, median *int) {
		t.Helper()
		if got.Preset != preset || got.Basis != basis || (median == nil) != (got.MedianFirstReplyMinutes == nil) || (median != nil && *median != *got.MedianFirstReplyMinutes) {
			t.Fatalf("suggestion = %s %s median %s, want %s %s %s", got.Preset, got.Basis, minutesText(got.MedianFirstReplyMinutes), preset, basis, minutesText(median))
		}
	}
	ptr := func(n int) *int { return &n }

	// No import: Same day, suggested for most email teams.
	want(presetOf(admin), "same_day", "no_history", nil)
	// The import runs: still Same day, checked when it ends.
	insertImport(t, pool, slug, "running")
	want(presetOf(admin), "same_day", "import_running", nil)
	if _, err := pool.Exec(t.Context(), `UPDATE import_jobs SET state='finished',status='OK',finished_at=now() WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
	// Only conversations opened in the last 30 days count, and only those answered; a note is no answer.
	imported(40*24*time.Hour, minutes(5))
	imported(2*24*time.Hour, nil)
	want(presetOf(admin), "same_day", "no_history", nil)
	imported(3*24*time.Hour, minutes(150))
	imported(4*24*time.Hour, minutes(190))
	imported(5*24*time.Hour, minutes(250))
	want(presetOf(admin), "same_day", "history", ptr(190))
	// Up to an hour: Within the hour. Longer than 8 hours: Next business day.
	for range 4 {
		imported(24*time.Hour, minutes(30))
	}
	want(presetOf(admin), "within_the_hour", "history", ptr(30))
	for range 9 {
		imported(24*time.Hour, minutes(600))
	}
	want(presetOf(admin), "next_business_day", "history", ptr(600))
	// Another workspace's history never counts.
	want(presetOf(other), "same_day", "no_history", nil)

	// Every preset is there with its targets, in business minutes (a day is 9 hours, 09:00 to 18:00).
	presets := readSetupTargets(t, api, admin).Presets
	if len(presets) != 3 {
		t.Fatalf("presets = %+v", presets)
	}
	type pair = [2]int
	wantPresets := map[string]struct {
		name    string
		targets map[string]pair
	}{
		"within_the_hour":   {"Within the hour", map[string]pair{"urgent": {15, 120}, "high": {30, 240}, "normal": {60, 540}, "low": {240, 1080}}},
		"same_day":          {"Same day", map[string]pair{"urgent": {60, 240}, "high": {120, 540}, "normal": {480, 1620}, "low": {540, 2700}}},
		"next_business_day": {"Next business day", map[string]pair{"urgent": {240, 540}, "high": {540, 1080}, "normal": {540, 1620}, "low": {1080, 2700}}},
	}
	for i, key := range []string{"within_the_hour", "same_day", "next_business_day"} {
		preset := presets[i]
		if preset.Key != key || preset.Name != wantPresets[key].name {
			t.Fatalf("preset %d = %s %q, want %s %q", i, preset.Key, preset.Name, key, wantPresets[key].name)
		}
		for priority, target := range wantPresets[key].targets {
			if got := preset.Targets[priority]; got.FirstResponse != target[0] || got.Resolution != target[1] {
				t.Fatalf("%s %s = %d/%d, want %d/%d", key, priority, got.FirstResponse, got.Resolution, target[0], target[1])
			}
		}
	}
}

func minutesText(minutes *int) string {
	if minutes == nil {
		return "null"
	}
	return strconv.Itoa(*minutes)
}
