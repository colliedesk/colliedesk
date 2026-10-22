package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

const importNotFound = "No import with that id in this workspace. List them with colliedesk_imports_list."

func importPath(id, rest string) string {
	return "/api/v1/imports/" + url.PathEscape(id) + rest
}

func importsList(c *call, _ map[string]any) (any, error) {
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := c.get("/api/v1/imports", nil, &list); err != nil {
		return nil, err
	}
	if list.Items == nil {
		list.Items = []json.RawMessage{}
	}
	return map[string]any{"imports": list.Items}, nil
}

type importIDArgument struct {
	ID string `json:"import_id"`
}

func importGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[importIDArgument](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = importNotFound
	var job json.RawMessage
	if err := c.get(importPath(args.ID, ""), nil, &job); err != nil {
		return nil, err
	}
	return map[string]any{"import": job}, nil
}

// importAnswer runs a change and returns the job it answers.
func importAnswer(c *call, method, path string, body any) (any, error) {
	c.notFound = importNotFound
	var job json.RawMessage
	if err := c.send(method, path, body, &job); err != nil {
		return nil, err
	}
	return map[string]any{"import": job}, nil
}

func importPreview(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Domain                         string `json:"domain"`
		APIKey                         string `json:"api_key"`
		RecentDays                     *int   `json:"recent_days"`
		IncludePersonalCannedResponses bool   `json:"include_personal_canned_responses"`
		HelpCenterPortal               int64  `json:"help_center_portal"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	options := map[string]any{"include_personal_canned_responses": args.IncludePersonalCannedResponses}
	if args.RecentDays != nil {
		options["recent_days"] = *args.RecentDays
	}
	if args.HelpCenterPortal > 0 {
		options["help_center_portal"] = args.HelpCenterPortal
	}
	return importAnswer(c, http.MethodPost, "/api/v1/imports", map[string]any{"source": "freshdesk", "domain": args.Domain, "api_key": args.APIKey, "options": options})
}

func importStart(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[importIDArgument](arguments)
	if err != nil {
		return nil, err
	}
	return importAnswer(c, http.MethodPost, importPath(args.ID, "/start"), nil)
}

func importCancel(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[importIDArgument](arguments)
	if err != nil {
		return nil, err
	}
	return importAnswer(c, http.MethodPost, importPath(args.ID, "/cancel"), nil)
}

func importKeySet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID     string `json:"import_id"`
		APIKey string `json:"api_key"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return importAnswer(c, http.MethodPut, importPath(args.ID, "/key"), map[string]any{"api_key": args.APIKey})
}

func importReport(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID     string `json:"import_id"`
		Format string `json:"format"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No report for that import yet, or no import with that id. Check its state with colliedesk_import_get."
	if args.Format == "markdown" {
		_, body, err := c.download(importPath(args.ID, "/report?format=markdown"), 4<<20)
		if err != nil {
			return nil, err
		}
		return map[string]any{"markdown": string(body)}, nil
	}
	var report json.RawMessage
	if err := c.get(importPath(args.ID, "/report"), nil, &report); err != nil {
		return nil, err
	}
	return map[string]any{"report": report}, nil
}

func importUndoRequest(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[importIDArgument](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = importNotFound
	return ask(c, http.MethodPost, importPath(args.ID, "/undo"), nil)
}
