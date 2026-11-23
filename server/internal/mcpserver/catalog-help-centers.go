package mcpserver

import (
	"net/http"
	"net/url"
)

const (
	// helpCenterIDProperty names the help center a settings tool works on; left out, the primary one.
	helpCenterIDProperty = `"help_center_id":{"type":"string",` + uuid + `,"description":"One of colliedesk_help_centers_list; left out, the primary help center."}`
	// helpCenterOnlyInput is the input of a settings tool that takes nothing but the help center.
	helpCenterOnlyInput = `{"type":"object","additionalProperties":false,"properties":{` + helpCenterIDProperty + `}}`

	helpCenterOutput = `{"type":"object","required":["help_center"],"properties":{"help_center":` + helpCenterSchema + `}}`

	helpCenterHandleRule = `handle is the path below the primary's address it is served at (hardware serves at <primary url>/hardware): 1 to 40 lowercase letters, digits or hyphens, ` +
		`starting with a letter or digit, unique in the workspace, neither a page of the site (a, c, search, support...) nor a language tag such as vi or pt-br. `
)

func helpCentersListSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_centers_list",
		Title: "List the help centers",
		Description: `List the workspace's help centers, the primary first: each one's id, name, handle, whether it is on, its title, language and public address (url). ` +
			`The primary serves the workspace's own address; each other one serves below it at /<handle>, or on its own custom domain. limit is how many a workspace may have. ` +
			`Pass an id as help_center_id to the colliedesk_help_center_* settings tools and colliedesk_kb_order_set.`,
		REST:   []string{"GET /api/v1/help-centers"},
		Input:  noArguments,
		Output: `{"type":"object","required":["help_centers","limit"],"properties":{"help_centers":{"type":"array","items":` + helpCenterSchema + `},"limit":{"type":"integer"}}}`,
		run:    helpCentersList,
	}
}

func helpCenterCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_create",
		Title: "Add a help center",
		Description: `Add a help center besides the primary one, for another product or audience. ` + helpCenterHandleRule +
			`It starts off unless enabled is true, and shows no collection until colliedesk_kb_collection_create or colliedesk_kb_collection_update names it in help_center_ids. ` +
			`A workspace has at most 20 help centers, the primary included (help_center_limit). Remove it with colliedesk_help_center_delete.`,
		REST: []string{"POST /api/v1/help-centers"},
		Input: `{"type":"object","additionalProperties":false,"required":["name","handle","locale"],"properties":{` +
			`"name":{"type":"string","minLength":1,"maxLength":100,"description":"What the team calls it in the app."},"handle":{"type":"string","minLength":1,"maxLength":40},` +
			`"title":{"type":"string","maxLength":200,"description":"Shown to readers; empty uses the workspace name."},"locale":{"type":"string","minLength":2,"maxLength":35},` +
			`"enabled":{"type":"boolean"},` + kbHelpCenterShowTagsProperty + `}}`,
		Output: helpCenterOutput,
		run:    helpCenterCreate,
	}
}

func helpCenterDeleteSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_delete",
		Title: "Delete a help center",
		Description: `Delete a help center other than the primary one: its pages go off line at once and its custom domain is released, so another help center may claim it. ` +
			`Its collections and articles stay in the knowledge base and on the other help centers they are shown on. It cannot be undone. ` +
			`One that serves on a custom domain also needs kb_articles:publish. The primary cannot be deleted; turn it off with colliedesk_help_center_update. ` +
			`One a live help chat or web form serves is refused (help_center_in_use, naming the channels): move each with colliedesk_channels_update and help_center_id first.`,
		REST:   []string{"DELETE /api/v1/help-centers/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["help_center_id"],"properties":{` + helpCenterIDProperty + `}}`,
		Output: `{"type":"object","required":["help_center_id","deleted"],"properties":{"help_center_id":{"type":"string"},"deleted":{"type":"boolean"}}}`,
		run:    helpCenterDelete,
	}
}

func helpCenterNotFound(id string) string {
	return "No help center " + id + " in this workspace. colliedesk_help_centers_list lists them."
}

// atHelpCenter takes help_center_id out of the arguments and names that help center in the query of
// path; without it the path is the primary's.
func (c *call) atHelpCenter(path string, arguments map[string]any) (string, map[string]any) {
	id, _ := arguments["help_center_id"].(string)
	if id == "" {
		return path, arguments
	}
	rest := make(map[string]any, len(arguments))
	for field, value := range arguments {
		if field != "help_center_id" {
			rest[field] = value
		}
	}
	c.notFound = helpCenterNotFound(id)
	return path + "?help_center_id=" + url.QueryEscape(id), rest
}

func helpCentersList(c *call, _ map[string]any) (any, error) {
	var listed map[string]any
	if err := c.get("/api/v1/help-centers", nil, &listed); err != nil {
		return nil, err
	}
	return listed, nil
}

func helpCenterCreate(c *call, arguments map[string]any) (any, error) {
	var created map[string]any
	if err := c.send(http.MethodPost, "/api/v1/help-centers", arguments, &created); err != nil {
		return nil, err
	}
	return map[string]any{"help_center": created}, nil
}

func helpCenterDelete(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["help_center_id"].(string)
	c.notFound = helpCenterNotFound(id)
	if err := c.send(http.MethodDelete, "/api/v1/help-centers/"+url.PathEscape(id), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"help_center_id": id, "deleted": true}, nil
}

func helpCenterGet(c *call, arguments map[string]any) (any, error) {
	path := "/api/v1/help-center"
	if id, _ := arguments["help_center_id"].(string); id != "" {
		c.notFound = helpCenterNotFound(id)
		path = "/api/v1/help-centers/" + url.PathEscape(id)
	}
	var center map[string]any
	if err := c.get(path, nil, &center); err != nil {
		return nil, err
	}
	return map[string]any{"help_center": center}, nil
}

// helpCenterUpdate changes the primary through PUT /help-center, which also holds the workspace-wide
// require_approval, and any help center by id through PATCH /help-centers/{id}, which also renames it.
func helpCenterUpdate(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["help_center_id"].(string)
	_, name := arguments["name"]
	_, handle := arguments["handle"]
	_, approval := arguments["require_approval"]
	_, feedback := arguments["feedback_conversations"]
	method, path, body := http.MethodPut, "/api/v1/help-center", arguments
	switch {
	case id == "" && (name || handle):
		return nil, &toolError{"name and handle belong to one help center: give its help_center_id (colliedesk_help_centers_list). The primary has no handle."}
	case id != "" && approval:
		return nil, &toolError{"require_approval is workspace-wide, not per help center: leave help_center_id out to change it."}
	case id != "" && feedback:
		return nil, &toolError{"feedback_conversations is workspace-wide, not per help center: leave help_center_id out to change it."}
	case id != "":
		c.notFound = helpCenterNotFound(id)
		method, path = http.MethodPatch, "/api/v1/help-centers/"+url.PathEscape(id)
		_, body = c.atHelpCenter("", arguments)
	}
	var center map[string]any
	if err := c.send(method, path, body, &center); err != nil {
		return nil, err
	}
	return map[string]any{"help_center": center}, nil
}
