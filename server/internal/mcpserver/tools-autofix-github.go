package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

const (
	installationNotFound = "No GitHub installation with that id in this workspace. Read them with colliedesk_autofix_settings_get."
	autofixRepoNotFound  = "No connected repository with that id in this workspace. Read them with colliedesk_autofix_settings_get."
)

type autofixGitHubArguments struct {
	InstallationID   string  `json:"installation_id"`
	RepoID           string  `json:"repo_id"`
	GitHubRepoID     int64   `json:"github_repo_id"`
	DefaultConnector *string `json:"default_connector"`
}

func autofixRepoPath(id, rest string) string {
	return "/api/v1/autofix/repos/" + url.PathEscape(id) + rest
}

func autofixSettingsGet(c *call, _ map[string]any) (any, error) {
	var settings json.RawMessage
	if err := c.get("/api/v1/autofix/settings", nil, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func githubRepositoriesList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = installationNotFound
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := c.get("/api/v1/autofix/github/installations/"+url.PathEscape(args.InstallationID)+"/repositories", nil, &list); err != nil {
		return nil, err
	}
	if list.Items == nil {
		list.Items = []json.RawMessage{}
	}
	return map[string]any{"repositories": list.Items}, nil
}

func githubConnect(c *call, _ map[string]any) (any, error) {
	out, err := passOn(c, http.MethodPost, "/api/v1/autofix/github/installations", nil)
	if err != nil {
		return nil, err
	}
	out["next"] = forThePerson(c, "it opens GitHub's install page, where they choose Only select repositories and install the app as themselves. colliedesk_autofix_settings_get then lists the installation.")
	return out, nil
}

func githubDisconnect(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = installationNotFound
	var settings json.RawMessage
	if err := c.send(http.MethodDelete, "/api/v1/autofix/github/installations/"+url.PathEscape(args.InstallationID), nil, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func autofixRepoConnect(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "That installation does not exist in this workspace, or it cannot reach that repository: list them with colliedesk_github_repositories_list."
	body := map[string]any{"installation_id": args.InstallationID, "github_repo_id": args.GitHubRepoID}
	if args.DefaultConnector != nil {
		body["default_connector"] = *args.DefaultConnector
	}
	var repo json.RawMessage
	if err := c.send(http.MethodPost, "/api/v1/autofix/repos", body, &repo); err != nil {
		return nil, err
	}
	return map[string]any{"repo": repo}, nil
}

func autofixRepoUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = autofixRepoNotFound
	var repo json.RawMessage
	if err := c.send(http.MethodPatch, autofixRepoPath(args.RepoID, ""), map[string]any{"default_connector": args.DefaultConnector}, &repo); err != nil {
		return nil, err
	}
	return map[string]any{"repo": repo}, nil
}

func autofixRepoDisconnect(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = autofixRepoNotFound
	if err := c.send(http.MethodDelete, autofixRepoPath(args.RepoID, ""), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"disconnected": true}, nil
}

func autofixRepoNotesIncludeSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = autofixRepoNotFound
	var repo json.RawMessage
	if err := c.send(http.MethodPut, autofixRepoPath(args.RepoID, "/include-notes"), nil, &repo); err != nil {
		return nil, err
	}
	return map[string]any{"repo": repo}, nil
}

func autofixRepoNotesExclude(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[autofixGitHubArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = autofixRepoNotFound
	var repo json.RawMessage
	if err := c.send(http.MethodDelete, autofixRepoPath(args.RepoID, "/include-notes"), nil, &repo); err != nil {
		return nil, err
	}
	return map[string]any{"repo": repo}, nil
}
