package auth

import "slices"

// Agent tokens made before roles (sessions.legacy) keep what their tier or list held then, frozen here
// so nothing a later release does widens them, minus what always waited for a person's approval
// (legacyNeverDirect): with agent requests gone, keeping those would let an old token export, invite or
// pay with no one deciding. A token missing one is told to make a new token. A tier below admin acted
// with the agent role (legacyAgentCeiling), so it keeps that limit too; an admin token, and a custom one
// holding an admin-tier permission, acted with its creator's role, which the person's roles now are.
// TestLegacyTokensLoseExactlyWhatAlwaysAskedAPerson checks these against the frozen tables of the
// release before roles (httpapi/testdata/legacy-token-access.json).

// legacyPresets is what each old tier held.
var legacyPresets = map[string][]string{
	"read": {
		"conversations:read", "messages:read", "ai_assist:read", "contacts:read", "tags:read", "saved_replies:read",
		"saved_views:read", "kb_articles:read", "kb_media:read", "help_center:read", "results:read", "team:read",
		"channels:read", "ai_settings:read", "autofix:read", "autofix_settings:read",
	},
	"draft": {
		"conversations:read", "conversations:update", "messages:read", "messages:note", "ai_assist:read",
		"ai_assist:create", "contacts:read", "tags:read", "tags:create", "saved_replies:read", "saved_views:read",
		"saved_views:create", "saved_views:update", "saved_views:delete", "kb_articles:read", "kb_media:read",
		"help_center:read", "results:read", "profile:update", "team:read", "channels:read", "ai_settings:read",
		"autofix:read", "autofix:update", "autofix_settings:read",
	},
	"send": {
		"conversations:read", "conversations:update", "messages:read", "messages:note", "messages:send",
		"ai_assist:read", "ai_assist:create", "contacts:read", "contacts:create", "contacts:update", "tags:read",
		"tags:create", "tags:update", "tags:delete", "saved_replies:read", "saved_replies:create",
		"saved_replies:update", "saved_replies:delete", "saved_views:read", "saved_views:create",
		"saved_views:update", "saved_views:delete", "kb_articles:read", "kb_articles:create", "kb_articles:update",
		"kb_articles:delete", "kb_articles:publish", "kb_media:read", "kb_media:create", "kb_media:update",
		"kb_media:delete", "help_center:read", "results:read", "profile:update", "team:read", "channels:read",
		"ai_settings:read", "autofix:read", "autofix:update", "autofix:send", "autofix_settings:read",
	},
	"admin": {
		"conversations:read", "conversations:update", "messages:read", "messages:note", "messages:send",
		"ai_assist:read", "ai_assist:create", "contacts:read", "contacts:create", "contacts:update", "tags:read",
		"tags:create", "tags:update", "tags:delete", "saved_replies:read", "saved_replies:create",
		"saved_replies:update", "saved_replies:delete", "saved_views:read", "saved_views:create",
		"saved_views:update", "saved_views:delete", "kb_articles:read", "kb_articles:create", "kb_articles:update",
		"kb_articles:delete", "kb_articles:publish", "kb_media:read", "kb_media:create", "kb_media:update",
		"kb_media:delete", "kb_media:purge", "help_center:read", "help_center:update", "results:read",
		"results:share", "profile:update", "team:read", "team:invite", "team:revoke", "team:manage_roles",
		"team:create_tokens", "channels:read", "channels:update", "channels:manage_credentials", "workspace:update",
		"workspace:export", "billing:read", "billing:manage", "imports:read", "imports:run",
		"imports:manage_credentials", "imports:undo", "ai_settings:read", "ai_settings:update",
		"ai_settings:manage_credentials", "autofix:read", "autofix:update", "autofix:send", "autofix_settings:read",
		"autofix_settings:update", "autofix_settings:manage_credentials",
	},
}

// legacyNeverDirect always waited for a person whatever the token held.
var legacyNeverDirect = []string{
	"ai_settings:update", "autofix:send", "autofix_settings:update", "billing:manage",
	"channels:manage_credentials", "channels:update", "imports:undo", "results:share", "team:create_tokens",
	"team:invite", "team:manage_roles", "team:revoke", "workspace:export", "workspace:update",
}

// legacyAgentCeiling is what the agent role held for a token below the admin tier.
var legacyAgentCeiling = legacyPresets["send"]

// legacyGiven is what a token made before roles is given now: tier is its old scope (read, draft, send,
// admin or custom) and custom its own list.
func legacyGiven(tier string, custom []string) PermissionSet {
	var held []string
	ceilingAgent := true
	if tier == "custom" {
		held = custom
		for _, name := range custom {
			if slices.Contains(legacyPresets["admin"], name) && !slices.Contains(legacyPresets["send"], name) {
				ceilingAgent = false
			}
		}
	} else {
		held = legacyPresets[tier]
		ceilingAgent = tier != "admin"
	}
	given := NewPermissionSet(held)
	for _, name := range legacyNeverDirect {
		delete(given, name)
	}
	if ceilingAgent {
		given = given.Intersect(NewPermissionSet(legacyAgentCeiling))
	}
	return given
}
