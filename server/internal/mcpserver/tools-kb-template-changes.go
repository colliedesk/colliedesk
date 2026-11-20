package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// kbTemplateChange copies the fields the tool was given, so an absent field stays as it is.
func kbTemplateChange(arguments map[string]any, change map[string]any) map[string]any {
	for _, key := range []string{"name", "description", "body"} {
		if value, ok := arguments[key]; ok {
			change[key] = value
		}
	}
	return change
}

func kbTemplateCreate(c *call, arguments map[string]any) (any, error) {
	var created restKBTemplate
	if err := c.send(http.MethodPost, "/api/v1/kb/templates", kbTemplateChange(arguments, map[string]any{}), &created); err != nil {
		return nil, err
	}
	return map[string]any{"template": kbTemplateView(created)}, nil
}

func kbTemplateUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"template_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	change := kbTemplateChange(arguments, map[string]any{"updated_at": args.UpdatedAt})
	if len(change) == 1 {
		return nil, &toolError{"Give something to change: name, description, body or several."}
	}
	c.forTemplate(args.ID)
	var updated restKBTemplate
	if err := c.send(http.MethodPatch, kbTemplatePath(args.ID), change, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"template": kbTemplateView(updated)}, nil
}

func kbTemplateDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"template_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forTemplate(args.ID)
	if err := c.send(http.MethodDelete, kbTemplatePath(args.ID, "?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"template_id": args.ID, "in_trash": true,
		"next": "The template is in the trash for 30 days, and no collection starts new articles from it. Bring it back with colliedesk_kb_template_undelete (template_id " + args.ID + ")."}, nil
}

func kbTemplateUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"template_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No template " + args.ID + " in the trash of this workspace. List the trash with colliedesk_kb_templates_list (trash true)."
	var restored restKBTemplate
	if err := c.send(http.MethodPost, kbTemplatePath(args.ID, "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return map[string]any{"template": kbTemplateView(restored)}, nil
}

func kbTemplateHistory(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID            string `json:"template_id"`
		Limit         int    `json:"limit"`
		BeforeVersion int    `json:"before_version"`
		Version       int    `json:"version"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forTemplate(args.ID)
	query := url.Values{}
	switch {
	case args.Version > 0:
		query.Set("limit", "1")
		query.Set("before_version", strconv.Itoa(args.Version+1))
	default:
		if args.Limit > 0 {
			query.Set("limit", strconv.Itoa(args.Limit))
		}
		if args.BeforeVersion > 0 {
			query.Set("before_version", strconv.Itoa(args.BeforeVersion))
		}
	}
	var page struct {
		Items []struct {
			restSavedReplyChange
			Name        string `json:"name"`
			Description string `json:"description"`
			Body        string `json:"body"`
		} `json:"items"`
		NextBeforeVersion *int `json:"next_before_version"`
	}
	if err := c.get(kbTemplatePath(args.ID, "/versions"), query, &page); err != nil {
		return nil, err
	}
	type versionOut struct {
		savedReplyChangeOut
		TeamText      kbTemplateText `json:"team_text"`
		BodyTruncated bool           `json:"body_truncated"`
	}
	versions := make([]versionOut, 0, len(page.Items))
	for _, item := range page.Items {
		body, truncated := item.Body, false
		if args.Version == 0 {
			body, truncated = cutReplyBody(body)
		}
		versions = append(versions, versionOut{*changeOut(&item.restSavedReplyChange), kbTemplateText{item.Name, item.Description, body}, truncated})
	}
	if args.Version > 0 {
		if len(versions) == 0 || versions[0].Version != args.Version {
			return nil, &toolError{fmt.Sprintf("Template %s has no version %d. See colliedesk_kb_template_history.", args.ID, args.Version)}
		}
		page.NextBeforeVersion = nil
	}
	return map[string]any{"template_id": args.ID, "versions": versions, "next_before_version": page.NextBeforeVersion}, nil
}

func kbTemplateVersionRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"template_id"`
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No template " + args.ID + " with version " + strconv.Itoa(args.Version) +
		" in this workspace (a template in the trash must be undeleted first). See colliedesk_kb_template_history or colliedesk_kb_templates_list."
	var restored restKBTemplate
	if err := c.send(http.MethodPost, kbTemplatePath(args.ID, "/versions/", strconv.Itoa(args.Version), "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return map[string]any{"template": kbTemplateView(restored)}, nil
}
