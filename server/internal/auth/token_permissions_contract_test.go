package auth

import (
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

func TestPresetAndCustomTokensAllowWhatTheyHold(t *testing.T) {
	person := Principal{}
	send := Principal{Scope: ScopeSend}
	custom := Principal{Scope: ScopeCustom, Permissions: []string{PermKBArticlesUpdate}}
	for _, item := range []struct {
		who        Principal
		permission string
		want       bool
	}{
		{person, PermKBMediaPurge, true},
		{send, PermMessagesSend, true},
		{send, PermKBMediaPurge, false},
		{send, "conversations:fly", false},
		{custom, PermKBArticlesUpdate, true},
		{custom, PermKBArticlesRead, false},
	} {
		if got := item.who.Allows(item.permission); got != item.want {
			t.Errorf("%q %v allows %s = %v, want %v", item.who.Scope, item.who.Permissions, item.permission, got, item.want)
		}
	}
	if NeedsAdmin(ScopeCustom, []string{PermKBArticlesRead}) || !NeedsAdmin(ScopeCustom, []string{PermKBArticlesRead, PermKBMediaPurge}) || !NeedsAdmin(ScopeAdmin, nil) {
		t.Error("a token acts as an admin exactly when it holds an admin-only permission or is an admin token")
	}
}
