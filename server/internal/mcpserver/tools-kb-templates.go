package mcpserver

import (
	"net/url"
	"strconv"
	"strings"
)

// restKBTemplate is a template, a trashed one, or a version: the fields each answer has.
type restKBTemplate struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Body        string                `json:"body"`
	UpdatedAt   string                `json:"updated_at"`
	LastChange  *restSavedReplyChange `json:"last_change"`
	DeletedAt   string                `json:"deleted_at"`
	DeletedBy   *restPerson           `json:"deleted_by"`
	PurgesAt    string                `json:"purges_at"`
}

type kbTemplateText struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

type kbTemplateOut struct {
	ID            string               `json:"id"`
	UpdatedAt     string               `json:"updated_at"`
	TeamText      kbTemplateText       `json:"team_text"`
	BodyTruncated *bool                `json:"body_truncated,omitempty"`
	LastChange    *savedReplyChangeOut `json:"last_change"`
	DeletedAt     string               `json:"deleted_at,omitempty"`
	DeletedBy     *string              `json:"deleted_by,omitempty"`
	PurgesAt      string               `json:"purges_at,omitempty"`
}

func kbTemplateView(template restKBTemplate) kbTemplateOut {
	out := kbTemplateOut{ID: template.ID, UpdatedAt: template.UpdatedAt, TeamText: kbTemplateText{template.Name, template.Description, template.Body},
		LastChange: changeOut(template.LastChange), DeletedAt: template.DeletedAt, PurgesAt: template.PurgesAt}
	if template.DeletedBy != nil {
		out.DeletedBy = &template.DeletedBy.Name
	}
	return out
}

// kbTemplateListView cuts the text of each template, as a list answer does.
func kbTemplateListView(template restKBTemplate) kbTemplateOut {
	view := kbTemplateView(template)
	var truncated bool
	view.TeamText.Body, truncated = cutReplyBody(view.TeamText.Body)
	view.BodyTruncated = &truncated
	return view
}

func kbTemplatePath(id string, rest ...string) string {
	return "/api/v1/kb/templates/" + url.PathEscape(id) + strings.Join(rest, "")
}

// forTemplate words a 404 for the template the tool names.
func (c *call) forTemplate(id string) {
	c.notFound = "No template " + id + " in this workspace. List them with colliedesk_kb_templates_list (trash true for deleted ones)."
}

func (c *call) kbTemplates() ([]restKBTemplate, error) {
	var page struct {
		Items []restKBTemplate `json:"items"`
	}
	if err := c.get("/api/v1/kb/templates", nil, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *call) kbTemplateTrashPage(query url.Values) ([]restKBTemplate, *string, error) {
	var page struct {
		Items      []restKBTemplate `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := c.get("/api/v1/kb/templates/trash", query, &page); err != nil {
		return nil, nil, err
	}
	return page.Items, page.NextCursor, nil
}

func kbTemplatesList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Trash  bool   `json:"trash"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var items []restKBTemplate
	var next *string
	if args.Trash {
		query := url.Values{}
		if args.Limit > 0 {
			query.Set("limit", strconv.Itoa(args.Limit))
		}
		if args.Cursor != "" {
			query.Set("cursor", args.Cursor)
		}
		items, next, err = c.kbTemplateTrashPage(query)
	} else {
		items, err = c.kbTemplates()
	}
	if err != nil {
		return nil, err
	}
	out := make([]kbTemplateOut, 0, len(items))
	for _, item := range items {
		out = append(out, kbTemplateListView(item))
	}
	return map[string]any{"templates": out, "next_cursor": next}, nil
}

func kbTemplateGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string `json:"template_id"`
		Name string `json:"name"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if (args.ID == "") == (args.Name == "") {
		return nil, &toolError{"Give template_id or name, not both."}
	}
	matches := func(template restKBTemplate) bool {
		return template.ID == args.ID || (args.Name != "" && strings.EqualFold(template.Name, strings.TrimSpace(args.Name)))
	}
	live, err := c.kbTemplates()
	if err != nil {
		return nil, err
	}
	for _, item := range live {
		if matches(item) {
			return map[string]any{"template": kbTemplateView(item), "in_trash": false}, nil
		}
	}
	// A workspace holds at most 200 templates, the trash included, so a page or two covers it.
	query := url.Values{"limit": {"200"}}
	for {
		items, next, err := c.kbTemplateTrashPage(query)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if matches(item) {
				return map[string]any{"template": kbTemplateView(item), "in_trash": true}, nil
			}
		}
		if next == nil {
			break
		}
		query.Set("cursor", *next)
	}
	if args.ID != "" {
		c.forTemplate(args.ID)
		return nil, &toolError{c.notFound}
	}
	return nil, &toolError{"No template named " + strconv.Quote(args.Name) + " in this workspace or its trash. List them with colliedesk_kb_templates_list."}
}
