package mcpserver

// The Autofix settings: the Colliedesk GitHub App's installations and the repositories fixes go to.

const (
	installationIDProperty = `"installation_id":{"type":"string",` + uuid + `,"description":"The installation's id in Colliedesk (installations[].id of colliedesk_autofix_settings_get), not GitHub's number."}`
	autofixRepoIDProperty  = `"repo_id":{"type":"string",` + uuid + `,"description":"A connected repository's id (repos[].id of colliedesk_autofix_settings_get)."}`
	connectorEnum          = `"enum":["claude_issue","copilot_issue","cursor_api"]`

	autofixRepoSchema = `{"type":"object","required":["id","github_repo_id","full_name","visibility","include_notes","default_connector","paused_reason"],"properties":{` +
		`"id":{"type":"string"},"installation_id":{"type":["integer","null"]},"github_repo_id":{"type":"integer"},"full_name":{"type":"string"},` +
		`"visibility":{"type":"string","enum":["private","internal","public"]},"default_branch":{"type":"string"},"include_notes":{"type":"boolean"},` +
		`"default_connector":{"type":["string","null"]},"paused_reason":{"type":["string","null"],"description":"Why it takes no new fix: became_public, removed_from_app or installation_suspended."},` +
		`"paused_at":{"type":["string","null"]},"connected_by_name":{"type":"string"},"connected_at":{"type":"string"}}}`

	autofixSettingsSchema = `{"type":"object","required":["available","installations","repos","connectors"],"properties":{` +
		`"available":{"type":"boolean","description":"This server's GitHub App is set up."},"consent_version":{"type":"string"},"app_slug":{"type":["string","null"]},` +
		`"installations":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"installation_id":{"type":"integer"},"account_login":{"type":"string"},` +
		`"account_type":{"type":"string"},"repository_selection":{"type":"string","enum":["selected","all"]},` +
		`"status":{"type":"string","enum":["active","blocked_all_repositories","suspended"]},"settings_url":{"type":"string"}}}},` +
		`"repos":{"type":"array","items":` + autofixRepoSchema + `},"connectors":{"type":"array","items":{"type":"object"}}}}`
)

func autofixSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_autofix_settings_get",
		Title: "Show the Autofix settings",
		Description: `Show Autofix's settings: whether the server's GitHub App is set up (available), the GitHub installations (status blocked_all_repositories: it reaches every ` +
			`repository of its account and must be narrowed to selected ones on GitHub before a repository can be connected), the connected repositories (a paused one ` +
			`takes no new fix and says why) and the coding agents (Claude, Copilot, Cursor; never a key, only who set Cursor's and when).` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/autofix/settings"},
		Input:  noArguments,
		Output: autofixSettingsSchema,
		run:    autofixSettingsGet,
	}
}

func githubRepositoriesListSpec() Spec {
	return Spec{
		Name:  "colliedesk_github_repositories_list",
		Title: "List the repositories a GitHub installation reaches",
		Description: `List, read from GitHub now, the repositories an installation of the Colliedesk GitHub App can reach, each with its visibility and the connected ` +
			`repository it is (connected_repo_id, null when not connected). Only private ones can be connected with colliedesk_autofix_repo_connect.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/autofix/github/installations/{id}/repositories"},
		Input: `{"type":"object","additionalProperties":false,"required":["installation_id"],"properties":{` + installationIDProperty + `}}`,
		Output: `{"type":"object","required":["repositories"],"properties":{"repositories":{"type":"array","items":{"type":"object","required":["github_repo_id","full_name","visibility","connected_repo_id"],"properties":{` +
			`"github_repo_id":{"type":"integer"},"full_name":{"type":"string"},"visibility":{"type":"string","enum":["private","internal","public"]},"connected_repo_id":{"type":["string","null"]}}}}}}`,
		run: githubRepositoriesList,
	}
}

func githubConnectRequestSpec() Spec {
	return approvalSpec("colliedesk_github_connect_request", "Ask a person to connect GitHub for Autofix",
		`Ask an admin to install the Colliedesk GitHub App, so fixes can go to the workspace's private repositories. The person who approves gets GitHub's install page `+
			`(they choose Only select repositories there) and installs it as themselves; you never see the address. Needs the server's GitHub App (available in colliedesk_autofix_settings_get).`,
		[]string{"POST /api/v1/autofix/github/installations"}, noArguments, githubConnectRequest)
}

func githubDisconnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_github_disconnect",
		Title: "Disconnect a GitHub installation from Autofix",
		Description: `Uninstall the Colliedesk GitHub App from that account on GitHub and remove the installation. Its repositories stay, paused (removed_from_app), so their fixes ` +
			`still show where they went; fixes in progress on them are cancelled (repo_disconnected). To undo, an admin connects GitHub again (colliedesk_github_connect_request): ` +
			`the repositories come back. The cancelled fixes cannot be undone (send them again). Answers the settings.` + "\n\nTier: admin.",
		Tier: "admin", Destructive: true,
		REST:   []string{"DELETE /api/v1/autofix/github/installations/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["installation_id"],"properties":{` + installationIDProperty + `}}`,
		Output: autofixSettingsSchema,
		run:    githubDisconnect,
	}
}

func autofixRepoConnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_autofix_repo_connect",
		Title: "Connect a repository for Autofix",
		Description: `Connect a private repository of an active installation (github_repo_id from colliedesk_github_repositories_list), after Colliedesk reads its visibility from ` +
			`GitHub now: a public or internal one is refused (repo_not_private), and so is any while the installation reaches every repository (installation_blocked). ` +
			`default_connector is the coding agent a new fix on it suggests. Connecting one already connected resumes it. Undo with colliedesk_autofix_repo_disconnect.` + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"POST /api/v1/autofix/repos"},
		Input: `{"type":"object","additionalProperties":false,"required":["installation_id","github_repo_id"],"properties":{` + installationIDProperty + `,` +
			`"github_repo_id":{"type":"integer","minimum":1},"default_connector":{"type":"string",` + connectorEnum + `}}}`,
		Output: `{"type":"object","required":["repo"],"properties":{"repo":` + autofixRepoSchema + `}}`,
		run:    autofixRepoConnect,
	}
}

func autofixRepoUpdateSpec() Spec {
	return Spec{
		Name:        "colliedesk_autofix_repo_update",
		Title:       "Change the coding agent a repository suggests",
		Description: `Set the coding agent a new fix on a connected repository suggests (default_connector), or null for none.` + "\n\nTier: admin.",
		Tier:        "admin", Idempotent: true,
		REST: []string{"PATCH /api/v1/autofix/repos/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["repo_id","default_connector"],"properties":{` + autofixRepoIDProperty + `,` +
			`"default_connector":{"type":["string","null"],"enum":["claude_issue","copilot_issue","cursor_api",null]}}}`,
		Output: `{"type":"object","required":["repo"],"properties":{"repo":` + autofixRepoSchema + `}}`,
		run:    autofixRepoUpdate,
	}
}

func autofixRepoDisconnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_autofix_repo_disconnect",
		Title: "Disconnect a repository from Autofix",
		Description: `Disconnect a repository: no new fix goes to it, and its fixes in progress are cancelled (repo_disconnected); its fixes keep the repository's name. ` +
			`Connect it again with colliedesk_autofix_repo_connect; the cancelled fixes cannot be undone (send them again).` + "\n\nTier: admin.",
		Tier: "admin", Destructive: true,
		REST:   []string{"DELETE /api/v1/autofix/repos/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["repo_id"],"properties":{` + autofixRepoIDProperty + `}}`,
		Output: `{"type":"object","required":["disconnected"],"properties":{"disconnected":{"type":"boolean"}}}`,
		run:    autofixRepoDisconnect,
	}
}

func autofixRepoNotesIncludeRequestSpec() Spec {
	return approvalSpec("colliedesk_autofix_repo_notes_include_request", "Ask to let fixes on a repository carry internal notes",
		`Ask an admin to let a fix on this repository carry the internal notes its sender picks (never a note an agent token wrote). Notes then leave the workspace for the coding agent, so a person always decides. Undo with colliedesk_autofix_repo_notes_exclude.`,
		[]string{"PUT /api/v1/autofix/repos/{id}/include-notes"},
		`{"type":"object","additionalProperties":false,"required":["repo_id"],"properties":{`+autofixRepoIDProperty+`}}`, autofixRepoNotesIncludeRequest)
}

func autofixRepoNotesExcludeSpec() Spec {
	return Spec{
		Name:        "colliedesk_autofix_repo_notes_exclude",
		Title:       "Stop internal notes going with fixes on a repository",
		Description: `Stop internal notes going with fixes on a connected repository (the default). Fixes already sent keep what they carried.` + "\n\nTier: admin.",
		Tier:        "admin", Idempotent: true,
		REST:   []string{"DELETE /api/v1/autofix/repos/{id}/include-notes"},
		Input:  `{"type":"object","additionalProperties":false,"required":["repo_id"],"properties":{` + autofixRepoIDProperty + `}}`,
		Output: `{"type":"object","required":["repo"],"properties":{"repo":` + autofixRepoSchema + `}}`,
		run:    autofixRepoNotesExclude,
	}
}
