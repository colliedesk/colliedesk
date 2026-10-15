package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// call is one tool invocation: the caller's token, the REST client and the lookups several tools share.
// Lookups are fetched at most once per call.
type call struct {
	ctx   context.Context
	rest  *restClient
	token string
	spec  Spec

	// notFound words a 404 of any call this tool makes, for example "No conversation <id> in this workspace."
	notFound string

	session *restMe
	agents  []restAgent
	tags    []restTag
}

// get fetches path as the caller and decodes the 2xx answer. A failure comes back worded for a model.
func (c *call) get(path string, query url.Values, into any) error {
	return c.refuse(c.rest.do(c.ctx, c.token, http.MethodGet, path, query, nil, into), c.notFound)
}

func (c *call) send(method, path string, body, into any) error {
	return c.refuse(c.rest.do(c.ctx, c.token, method, path, nil, body, into), c.notFound)
}

// me is the caller's identity, workspace and tier.
func (c *call) me() (*restMe, error) {
	if c.session == nil {
		var me restMe
		if err := c.get("/api/v1/auth/me", nil, &me); err != nil {
			return nil, err
		}
		c.session = &me
	}
	return c.session, nil
}

func (c *call) agentList() ([]restAgent, error) {
	if c.agents == nil {
		var page struct {
			Items []restAgent `json:"items"`
		}
		if err := c.get("/api/v1/agents", nil, &page); err != nil {
			return nil, err
		}
		c.agents = append([]restAgent{}, page.Items...)
	}
	return c.agents, nil
}

func (c *call) tagList() ([]restTag, error) {
	if c.tags == nil {
		var page struct {
			Items []restTag `json:"items"`
		}
		if err := c.get("/api/v1/tags", nil, &page); err != nil {
			return nil, err
		}
		c.tags = append([]restTag{}, page.Items...)
	}
	return c.tags, nil
}

// findTag finds an existing tag by name, ignoring case and surrounding spaces.
func (c *call) findTag(name string) (restTag, bool, error) {
	tags, err := c.tagList()
	if err != nil {
		return restTag{}, false, err
	}
	for _, tag := range tags {
		if strings.EqualFold(tag.Name, strings.TrimSpace(name)) {
			return tag, true, nil
		}
	}
	return restTag{}, false, nil
}

// tagByName finds an existing tag, or says which tags exist.
func (c *call) tagByName(name string) (restTag, error) {
	tag, found, err := c.findTag(name)
	if err != nil || found {
		return tag, err
	}
	tags, _ := c.tagList()
	names := make([]string, len(tags))
	for index, tag := range tags {
		names[index] = tag.Name
	}
	if len(names) == 0 {
		return restTag{}, &toolError{fmt.Sprintf("No tag named %q. This workspace has no tags yet.", name)}
	}
	return restTag{}, &toolError{fmt.Sprintf("No tag named %q. Existing tags: %s.", name, strings.Join(names, ", "))}
}

// createTag adds a tag to the workspace and remembers it for the rest of the call.
func (c *call) createTag(name string) (restTag, error) {
	var created restTag
	if err := c.send(http.MethodPost, "/api/v1/tags", map[string]any{"name": strings.TrimSpace(name)}, &created); err != nil {
		return restTag{}, err
	}
	c.tags = append(c.tags, created)
	return created, nil
}

// contactsByID fetches the contacts with the given ids in one call.
func (c *call) contactsByID(ids []string) (map[string]restContact, error) {
	found := make(map[string]restContact, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	var page struct {
		Items []restContact `json:"items"`
	}
	if err := c.get("/api/v1/contacts", url.Values{"ids": {strings.Join(ids, ",")}}, &page); err != nil {
		return nil, err
	}
	for _, contact := range page.Items {
		found[contact.ID] = contact
	}
	return found, nil
}

// agentNames maps agent ids to their rows.
func (c *call) agentsByID() (map[string]restAgent, error) {
	agents, err := c.agentList()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]restAgent, len(agents))
	for _, agent := range agents {
		byID[agent.ID] = agent
	}
	return byID, nil
}

// callTimeout bounds one tool call, REST round trips included.
const callTimeout = 25 * time.Second
