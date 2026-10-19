package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

type restSavedView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Scope        string          `json:"scope"`
	Filters      json.RawMessage `json:"filters"`
	OwnerAgentID *string         `json:"owner_agent_id"`
	UpdatedAt    string          `json:"updated_at"`
	DeletedAt    *string         `json:"deleted_at"`
	PurgesAt     *string         `json:"purges_at"`
}

type savedViewOut struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Scope     string          `json:"scope"`
	Mine      bool            `json:"mine"`
	Filters   json.RawMessage `json:"filters"`
	UpdatedAt string          `json:"updated_at"`
	DeletedAt string          `json:"deleted_at,omitempty"`
	PurgesAt  string          `json:"purges_at,omitempty"`
}

func (c *call) savedViewOut(view restSavedView) (savedViewOut, error) {
	me, err := c.me()
	if err != nil {
		return savedViewOut{}, err
	}
	out := savedViewOut{ID: view.ID, Name: view.Name, Scope: view.Scope, Filters: view.Filters, UpdatedAt: view.UpdatedAt,
		Mine: view.OwnerAgentID != nil && *view.OwnerAgentID == me.Agent.ID}
	if view.DeletedAt != nil {
		out.DeletedAt = *view.DeletedAt
	}
	if view.PurgesAt != nil {
		out.PurgesAt = *view.PurgesAt
	}
	return out, nil
}

func (c *call) savedViews(trash bool) ([]restSavedView, error) {
	path := "/api/v1/saved-views"
	if trash {
		path += "/trash"
	}
	var list struct {
		Items []restSavedView `json:"items"`
	}
	if err := c.get(path, nil, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func savedViewsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Trash bool `json:"trash"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	views, err := c.savedViews(args.Trash)
	if err != nil {
		return nil, err
	}
	out := make([]savedViewOut, 0, len(views))
	for _, view := range views {
		item, err := c.savedViewOut(view)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return map[string]any{"saved_views": out}, nil
}

// savedViewAnswer renders the view a write returned.
func (c *call) savedViewAnswer(method, path string, body any) (any, error) {
	c.notFound = "No saved view with that id that this token's person can see. List them with colliedesk_saved_views_list."
	var view restSavedView
	if err := c.send(method, path, body, &view); err != nil {
		return nil, err
	}
	out, err := c.savedViewOut(view)
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved_view": out}, nil
}

func savedViewPath(id string, rest string) string {
	return "/api/v1/saved-views/" + url.PathEscape(id) + rest
}

func savedViewCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Name    string          `json:"name"`
		Scope   string          `json:"scope"`
		Filters json.RawMessage `json:"filters"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return c.savedViewAnswer(http.MethodPost, "/api/v1/saved-views", map[string]any{"name": args.Name, "scope": args.Scope, "filters": args.Filters})
}

func savedViewUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string          `json:"saved_view_id"`
		UpdatedAt string          `json:"updated_at"`
		Name      *string         `json:"name"`
		Scope     *string         `json:"scope"`
		Filters   json.RawMessage `json:"filters"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	change := map[string]any{"updated_at": args.UpdatedAt}
	if args.Name != nil {
		change["name"] = *args.Name
	}
	if args.Scope != nil {
		change["scope"] = *args.Scope
	}
	if args.Filters != nil {
		change["filters"] = args.Filters
	}
	if len(change) == 1 {
		return nil, &toolError{"Nothing to change: pass a new name, scope or filters."}
	}
	return c.savedViewAnswer(http.MethodPatch, savedViewPath(args.ID, ""), change)
}

func savedViewDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"saved_view_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return c.savedViewAnswer(http.MethodDelete, savedViewPath(args.ID, ""), nil)
}

func savedViewRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"saved_view_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	return c.savedViewAnswer(http.MethodPost, savedViewPath(args.ID, "/restore"), nil)
}
