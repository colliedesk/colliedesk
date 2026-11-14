package auth

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// The contract's TokenPermission enum is this list in this order, so clients and the web form never
// drift from what the server checks.
func TestTokenPermissionEnumOfTheContractIsTheServersList(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	block := text[strings.Index(text, "\n    TokenPermission:"):]
	block = block[strings.Index(block, "      enum:\n")+len("      enum:\n"):]
	var contract []string
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "        - ") {
			break
		}
		contract = append(contract, strings.TrimSpace(strings.TrimPrefix(line, "        - ")))
	}
	var server []string
	for _, item := range TokenPermissions() {
		server = append(server, item.Name)
	}
	if !slices.Equal(contract, server) {
		t.Fatalf("contract TokenPermission = %v\nserver list = %v", contract, server)
	}
}

func admin() *string { system := "admin"; return &system }

func agentSystem() *string { system := "agent"; return &system }

func agentRole() roleRow {
	return roleRow{ID: "agent", Name: "Agent", System: agentSystem(), Permissions: AgentRolePermissions()}
}

func adminRole() roleRow { return roleRow{ID: "admin", Name: "Admin", System: admin()} }

func kbRole() roleRow {
	return roleRow{ID: "kb", Name: "Knowledge base editor", Permissions: []string{PermKBArticlesRead, PermKBArticlesUpdate, PermKBArticlesPublish}}
}

func principalOf(roles []roleRow, limit roleRow, scope string, permissions []string, legacy bool) Principal {
	var p Principal
	p.setAccess(roles, limit, scope, permissions, legacy)
	return p
}

func TestAPersonHoldsEveryPermissionOfEachRole(t *testing.T) {
	both := principalOf([]roleRow{agentRole(), kbRole()}, roleRow{}, "", nil, false)
	if !both.Can(PermMessagesSend) || !both.Can(PermKBArticlesPublish) || both.Can(PermKBMediaPurge) || both.Can("conversations:fly") {
		t.Fatalf("Agent and KB editor: %v", both.Effective())
	}
	if all := principalOf([]roleRow{adminRole()}, roleRow{}, "", nil, false); len(all.Effective()) != len(AllPermissions()) {
		t.Fatalf("Admin holds %d permissions, want every one (%d)", len(all.Effective()), len(AllPermissions()))
	}
	limited := principalOf([]roleRow{adminRole()}, agentRole(), "", nil, false)
	if limited.Can(PermKBMediaPurge) || !limited.Can(PermMessagesSend) || limited.IsToken() {
		t.Fatal("an admin's session limited to the Agent role holds only the Agent role's permissions")
	}
}

func TestATokenNeverOutranksItsPerson(t *testing.T) {
	custom := principalOf([]roleRow{agentRole()}, roleRow{}, ScopeCustom, []string{PermMessagesRead, PermKBMediaPurge}, false)
	if !custom.Can(PermMessagesRead) || custom.Can(PermKBMediaPurge) || custom.Can(PermMessagesSend) {
		t.Fatalf("a custom token of an agent: %v", custom.Effective())
	}
	if !custom.PersonCan(PermMessagesSend) {
		t.Fatal("PersonCan says what the person holds whatever the token was given")
	}
	// The person lost the role that held messages:read after the token was made.
	shrunk := principalOf([]roleRow{kbRole()}, roleRow{}, ScopeCustom, []string{PermMessagesRead, PermKBArticlesRead}, false)
	if shrunk.Can(PermMessagesRead) || !shrunk.Can(PermKBArticlesRead) {
		t.Fatalf("a custom token after its person lost a role: %v", shrunk.Effective())
	}
}

func TestARoleTokenFollowsItsRole(t *testing.T) {
	kb := kbRole()
	token := principalOf([]roleRow{agentRole(), kb}, kb, ScopeRole, nil, false)
	if !token.Can(PermKBArticlesPublish) || token.Can(PermMessagesSend) || token.Token.Kind != "role" || !token.Token.RoleHeld {
		t.Fatalf("a token following KB editor: %v %+v", token.Effective(), token.Token)
	}
	kb.Permissions = append(kb.Permissions, PermKBMediaRead)
	changed := principalOf([]roleRow{agentRole(), kb}, kb, ScopeRole, nil, false)
	if !changed.Can(PermKBMediaRead) {
		t.Fatal("a role token gets what its role gets")
	}
}

func TestARoleTokenStopsWhileItsPersonLacksAPermissionOfTheRole(t *testing.T) {
	exporter := roleRow{ID: "exporter", Name: "Exporter", Permissions: []string{PermConversationsRead, PermWorkspaceExport}}
	stopped := principalOf([]roleRow{agentRole()}, exporter, ScopeRole, nil, false)
	if len(stopped.Effective()) != 0 || stopped.Token.RoleHeld {
		t.Fatalf("a token whose person lacks workspace:export of its role may do %v", stopped.Effective())
	}
	if !slices.Contains(stopped.Token.Permissions, PermWorkspaceExport) {
		t.Fatal("a stopped token still says what its role gives")
	}
	back := principalOf([]roleRow{agentRole(), exporter}, exporter, ScopeRole, nil, false)
	if !back.Can(PermWorkspaceExport) || !back.Token.RoleHeld {
		t.Fatal("giving the role back starts the token again")
	}
}

// A token may follow a role its person does not hold while they hold everything it gives: an admin's
// token that follows Agent, or an agent's that follows a narrower role.
func TestARoleTokenFollowsARoleItsPersonDoesNotHoldButCovers(t *testing.T) {
	admins := principalOf([]roleRow{adminRole()}, agentRole(), ScopeRole, nil, false)
	if !admins.Token.RoleHeld || !admins.Can(PermMessagesSend) || admins.Can(PermWorkspaceExport) {
		t.Fatalf("an admin's token following Agent: %v %+v", admins.Effective(), admins.Token)
	}
	kb := kbRole()
	agents := principalOf([]roleRow{agentRole()}, kb, ScopeRole, nil, false)
	if !agents.Token.RoleHeld || !agents.Can(PermKBArticlesPublish) || agents.Can(PermMessagesSend) {
		t.Fatalf("an agent's token following %s: %v %+v", kb.Name, agents.Effective(), agents.Token)
	}
}

func TestLegacyTokensLoseExactlyWhatAlwaysAskedAPerson(t *testing.T) {
	data, err := os.ReadFile("../httpapi/testdata/legacy-token-access.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		Presets      map[string][]string `json:"presets"`
		NeverDirect  []string            `json:"never_direct"`
		AgentCeiling []string            `json:"agent_ceiling"`
	}
	if err := json.Unmarshal(data, &frozen); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(legacyNeverDirect, frozen.NeverDirect) {
		t.Fatalf("legacyNeverDirect = %v, frozen %v", legacyNeverDirect, frozen.NeverDirect)
	}
	if got := NewPermissionSet(legacyAgentCeiling).Sorted(); !slices.Equal(got, NewPermissionSet(frozen.AgentCeiling).Sorted()) {
		t.Fatalf("legacyAgentCeiling = %v, frozen %v", got, frozen.AgentCeiling)
	}
	everyone := []roleRow{adminRole()}
	for tier, held := range frozen.Presets {
		token := principalOf(everyone, roleRow{}, tier, nil, true)
		for _, name := range held {
			want := ValidPermission(name) && !slices.Contains(frozen.NeverDirect, name)
			if tier != "admin" {
				want = want && slices.Contains(frozen.AgentCeiling, name)
			}
			if got := token.Can(name); got != want {
				t.Errorf("an old %s token of an admin: Can(%s) = %v, want %v", tier, name, got, want)
			}
		}
		for _, name := range AllPermissions() {
			if !slices.Contains(held, name) && token.Can(name) {
				t.Errorf("an old %s token gained %s", tier, name)
			}
		}
		if token.Token.Kind != "legacy" || *token.Token.LegacyTier != tier {
			t.Errorf("an old %s token reads as %+v", tier, token.Token)
		}
	}
	purge := principalOf(everyone, roleRow{}, ScopeCustom, []string{PermKBMediaPurge, PermWorkspaceExport}, true)
	if !purge.Can(PermKBMediaPurge) || purge.Can(PermWorkspaceExport) {
		t.Fatalf("an old custom token with an admin-only permission: %v", purge.Effective())
	}
}

func TestTheAgentRoleStartsAsTheMigrationMadeIt(t *testing.T) {
	// The roles migration wrote the first list. A later migration may replace the function; the list
	// AgentRolePermissions must match is the last definition, which is what a new workspace gets.
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	var migration []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		data, err := os.ReadFile("../../migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "FUNCTION colliedesk_agent_role_permissions") {
			continue
		}
		block := text[strings.LastIndex(text, "SELECT ARRAY["):]
		block = block[:strings.Index(block, "]::text[]")]
		migration = nil
		for _, part := range strings.Split(block, "'") {
			if strings.Contains(part, ":") {
				migration = append(migration, part)
			}
		}
	}
	if !slices.Equal(migration, AgentRolePermissions()) {
		t.Fatalf("migration Agent role = %v\nauth.AgentRolePermissions = %v", migration, AgentRolePermissions())
	}
	if len(migration) != 46 {
		t.Fatalf("the Agent role starts with %d permissions, want 46", len(migration))
	}
}
