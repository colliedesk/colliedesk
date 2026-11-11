package mcpserver

import (
	"encoding/json"
	"net/http"
)

func resultsGet(c *call, _ map[string]any) (any, error) {
	var page json.RawMessage
	if err := c.get("/api/v1/results", nil, &page); err != nil {
		return nil, err
	}
	return map[string]any{"results": page}, nil
}

func resultsShareLinkGet(c *call, _ map[string]any) (any, error) {
	var state json.RawMessage
	if err := c.get("/api/v1/results/share-link", nil, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func resultsShareLinkCreate(c *call, _ map[string]any) (any, error) {
	out, err := passOn(c, http.MethodPost, "/api/v1/results/share-link", nil)
	if err != nil {
		return nil, err
	}
	out["next"] = "Anyone with url sees the results page's totals without signing in; the old link stopped working. Give it to the person you work for; revoke it with colliedesk_results_share_link_revoke."
	return out, nil
}

func resultsShareLinkRevoke(c *call, _ map[string]any) (any, error) {
	var state json.RawMessage
	if err := c.send(http.MethodDelete, "/api/v1/results/share-link", nil, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func resultsEmailSet(c *call, arguments map[string]any) (any, error) {
	var out map[string]any
	if err := c.send(http.MethodPatch, "/api/v1/agents/me/results-email", map[string]any{"weekly": arguments["weekly"]}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
