package mcpserver

import (
	"context"
	"errors"
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
	// wrote is set once a call of this tool has changed data, so a later failure can say so.
	wrote bool

	session *restMe
	agents  []restAgent
	tags    []restTag
}

// get fetches path as the caller and decodes the 2xx answer. A failure comes back worded for a model.
func (c *call) get(path string, query url.Values, into any) error {
	return c.refuse(c.rest.do(c.ctx, c.token, http.MethodGet, path, query, nil, into), c.notFound)
}

// download reads at most limit bytes of a file as the caller; a failure comes back worded for a model.
func (c *call) download(path string, limit int) (http.Header, []byte, error) {
	header, body, err := c.rest.download(c.ctx, c.token, path, limit)
	return header, body, c.refuse(err, c.notFound)
}

// send changes data as the caller; a failure comes back worded for a model, and a success is remembered.
func (c *call) send(method, path string, body, into any) error {
	err := c.rest.do(c.ctx, c.token, method, path, nil, body, into)
	if err == nil {
		c.wrote = true
	}
	return c.refuse(err, c.notFound)
}

// upload posts raw bytes as the caller; a failure comes back worded for a model, and a success is remembered.
func (c *call) upload(path, contentType string, data []byte, into any) error {
	err := c.rest.upload(c.ctx, c.token, path, contentType, data, into)
	if err == nil {
		c.wrote = true
	}
	return c.refuse(err, c.notFound)
}

// sendBytes sends raw bytes with method and headers as the caller.
func (c *call) sendBytes(method, path, contentType string, headers map[string]string, data []byte, into any) error {
	err := c.rest.sendBytes(c.ctx, c.token, method, path, contentType, headers, data, into)
	if err == nil {
		c.wrote = true
	}
	return c.refuse(err, c.notFound)
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

// findTag finds an existing tag by name. REST keeps names unique by exact spelling, so "Refund" and
// "refund" can both exist: the exact spelling wins; otherwise a name that differs only in case from
// exactly one tag means that tag; several such tags are ambiguous and the caller must give the exact
// name. A call never acts on a tag other than the one it named.
func (c *call) findTag(name string) (restTag, bool, error) {
	tags, err := c.tagList()
	if err != nil {
		return restTag{}, false, err
	}
	name = strings.TrimSpace(name)
	var sameLetters []restTag
	for _, tag := range tags {
		if tag.Name == name {
			return tag, true, nil
		}
		if strings.EqualFold(tag.Name, name) {
			sameLetters = append(sameLetters, tag)
		}
	}
	switch len(sameLetters) {
	case 0:
		return restTag{}, false, nil
	case 1:
		return sameLetters[0], true, nil
	}
	return restTag{}, false, &toolError{fmt.Sprintf("Several tags match %q when case is ignored: %s. Use the exact name of the one you mean.",
		name, strings.Join(tagNames(sameLetters), ", "))}
}

// tagByName finds an existing tag, or says which tags exist.
func (c *call) tagByName(name string) (restTag, error) {
	tag, found, err := c.findTag(name)
	if err != nil || found {
		return tag, err
	}
	tags, _ := c.tagList()
	names := tagNames(tags)
	if len(names) == 0 {
		return restTag{}, &toolError{fmt.Sprintf("No tag named %q. This workspace has no tags yet.", name)}
	}
	return restTag{}, &toolError{fmt.Sprintf("No tag named %q. Existing tags: %s.", name, strings.Join(names, ", "))}
}

// createTag adds a tag to the workspace and remembers it for the rest of the call. When the create is
// refused, another caller may have made the same tag since the list was read: it looks again and uses
// that tag when it is there.
func (c *call) createTag(name string) (restTag, error) {
	name = strings.TrimSpace(name)
	var created restTag
	err := c.rest.do(c.ctx, c.token, http.MethodPost, "/api/v1/tags", nil, map[string]any{"name": name}, &created)
	if err == nil {
		c.wrote = true
		c.tags = append(c.tags, created)
		return created, nil
	}
	var refused *restError
	if errors.As(err, &refused) && refused.Status == http.StatusBadRequest {
		c.tags = nil
		if tag, found, lookupErr := c.findTag(name); lookupErr == nil && found {
			return tag, nil
		}
	}
	return restTag{}, c.refuse(err, c.notFound)
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
