package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of macros. Titles and bodies count characters, not bytes.
const (
	MaxMacrosShared   = 500
	MaxMacrosPersonal = 100
	MaxMacroTitle     = 200
	MaxMacroBody      = 20000
	MaxMacroTags      = 50
	// MacroTrashRetention is how long a deleted macro stays restorable before it is gone.
	MacroTrashRetention = 30 * 24 * time.Hour
)

// Who sees a macro.
const (
	MacroPersonal = "personal"
	MacroShared   = "shared"
)

// Macro is a reply template plus the ticket actions an agent applies in one step. The body may hold
// {{customer.name}}, {{requester.name}}, {{agent.name}} and {{ticket.id}}; applying fills them in.
type Macro struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Body         string       `json:"body"`
	Scope        string       `json:"scope"`
	OwnerAgentID *string      `json:"owner_agent_id"`
	Actions      MacroActions `json:"actions"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	DeletedAt    *time.Time   `json:"deleted_at"`
	PurgesAt     *time.Time   `json:"purges_at"`
}

// MacroActions is what applying a macro changes. A field left out is left as it is. SetAssignee with
// a nil AssigneeID clears the assignee. There is no group to assign to.
type MacroActions struct {
	Status       *string  `json:"status,omitempty"`
	Priority     *string  `json:"priority,omitempty"`
	SetAssignee  bool     `json:"set_assignee,omitempty"`
	AssigneeID   *string  `json:"assignee_id,omitempty"`
	AddTagIDs    []string `json:"add_tag_ids,omitempty"`
	RemoveTagIDs []string `json:"remove_tag_ids,omitempty"`
}

// MacroContext is who a macro is filled in for.
type MacroContext struct {
	CustomerName string
	AgentName    string
	TicketID     string
}

var (
	ErrMacroNotFound         = errors.New("macro not found")
	ErrMacroTitleTaken       = errors.New("a macro with this title already exists")
	ErrMacroLimit            = errors.New("the workspace or the person already holds the most macros")
	ErrMacroChanged          = errors.New("this macro was changed by someone else")
	ErrMacroConversationGone = errors.New("conversation not found")
	ErrMacroTagUnknown       = errors.New("a tag on this macro is not in this workspace")
	ErrMacroAssignee         = errors.New("that agent is not an active member of this workspace")
	ErrMacroApplicationGone  = errors.New("macro application not found")
)

// InvalidMacroError says which part of a macro the API refuses, in words a person can act on.
type InvalidMacroError struct{ Message string }

func (e *InvalidMacroError) Error() string { return e.Message }

var (
	macroStatuses   = map[string]bool{"open": true, "pending": true, "resolved": true, "closed": true}
	macroPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
	macroIDPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// A space or tab before an empty variable goes with it, so "Hi {{customer.name}}," reads "Hi,".
	macroVariable = regexp.MustCompile(`([ \t]?)\{\{\s*(customer\.name|requester\.name|agent\.name|ticket\.id)\s*\}\}`)
)

// UnmarshalJSON treats a present assignee_id (including null) as a change of assignee, so a caller
// can send the id without a separate flag.
func (a *MacroActions) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	allowed := map[string]bool{"status": true, "priority": true, "set_assignee": true, "assignee_id": true, "add_tag_ids": true, "remove_tag_ids": true}
	for key := range raw {
		if !allowed[key] {
			return fmt.Errorf("unknown action %q", key)
		}
	}
	if value, ok := raw["status"]; ok {
		var status string
		if err := json.Unmarshal(value, &status); err != nil {
			return err
		}
		a.Status = &status
	}
	if value, ok := raw["priority"]; ok {
		var priority string
		if err := json.Unmarshal(value, &priority); err != nil {
			return err
		}
		a.Priority = &priority
	}
	if value, ok := raw["set_assignee"]; ok {
		if err := json.Unmarshal(value, &a.SetAssignee); err != nil {
			return err
		}
	}
	if value, ok := raw["assignee_id"]; ok {
		a.SetAssignee = true
		if string(value) != "null" {
			var id string
			if err := json.Unmarshal(value, &id); err != nil {
				return err
			}
			a.AssigneeID = &id
		}
	}
	if value, ok := raw["add_tag_ids"]; ok && string(value) != "null" {
		if err := json.Unmarshal(value, &a.AddTagIDs); err != nil {
			return err
		}
	}
	if value, ok := raw["remove_tag_ids"]; ok && string(value) != "null" {
		if err := json.Unmarshal(value, &a.RemoveTagIDs); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON writes assignee_id as null when the macro clears the assignee.
func (a MacroActions) MarshalJSON() ([]byte, error) {
	payload := map[string]any{}
	if a.Status != nil {
		payload["status"] = *a.Status
	}
	if a.Priority != nil {
		payload["priority"] = *a.Priority
	}
	if a.SetAssignee {
		payload["set_assignee"] = true
		payload["assignee_id"] = a.AssigneeID
	}
	if len(a.AddTagIDs) > 0 {
		payload["add_tag_ids"] = a.AddTagIDs
	}
	if len(a.RemoveTagIDs) > 0 {
		payload["remove_tag_ids"] = a.RemoveTagIDs
	}
	return json.Marshal(payload)
}

// Empty reports whether the actions change nothing.
func (a MacroActions) Empty() bool {
	return a.Status == nil && a.Priority == nil && !a.SetAssignee && len(a.AddTagIDs) == 0 && len(a.RemoveTagIDs) == 0
}

// CleanMacro checks a macro's title, body, scope and actions. The body is stored as written, variables
// unfilled. A macro needs text or at least one action.
func CleanMacro(title, body, scope string, actions MacroActions) (string, string, string, MacroActions, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", "", "", MacroActions{}, &InvalidMacroError{"A macro needs a title."}
	case utf8.RuneCountInString(title) > MaxMacroTitle:
		return "", "", "", MacroActions{}, &InvalidMacroError{fmt.Sprintf("A title holds at most %d characters.", MaxMacroTitle)}
	}
	if scope != MacroPersonal && scope != MacroShared {
		return "", "", "", MacroActions{}, &InvalidMacroError{"Scope is personal or shared."}
	}
	if utf8.RuneCountInString(body) > MaxMacroBody {
		return "", "", "", MacroActions{}, &InvalidMacroError{fmt.Sprintf("The text holds at most %d characters.", MaxMacroBody)}
	}
	body = strings.TrimRight(body, " \t\r\n")
	cleaned, err := cleanMacroActions(actions)
	if err != nil {
		return "", "", "", MacroActions{}, err
	}
	if strings.TrimSpace(body) == "" && cleaned.Empty() {
		return "", "", "", MacroActions{}, &InvalidMacroError{"A macro needs text or an action: a status, a priority, an assignee or a tag."}
	}
	return title, body, scope, cleaned, nil
}

func cleanMacroActions(actions MacroActions) (MacroActions, error) {
	if actions.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*actions.Status))
		if !macroStatuses[status] {
			return MacroActions{}, &InvalidMacroError{"Status is open, pending, resolved or closed."}
		}
		actions.Status = &status
	}
	if actions.Priority != nil {
		priority := strings.ToLower(strings.TrimSpace(*actions.Priority))
		if !macroPriorities[priority] {
			return MacroActions{}, &InvalidMacroError{"Priority is low, normal, high or urgent."}
		}
		actions.Priority = &priority
	}
	if actions.SetAssignee && actions.AssigneeID != nil {
		id := strings.ToLower(strings.TrimSpace(*actions.AssigneeID))
		if !macroIDPattern.MatchString(id) {
			return MacroActions{}, &InvalidMacroError{"Assignee is an agent id, or null to leave the conversation unassigned."}
		}
		actions.AssigneeID = &id
	}
	var err error
	if actions.AddTagIDs, err = cleanMacroTags(actions.AddTagIDs, "add"); err != nil {
		return MacroActions{}, err
	}
	if actions.RemoveTagIDs, err = cleanMacroTags(actions.RemoveTagIDs, "remove"); err != nil {
		return MacroActions{}, err
	}
	seen := map[string]bool{}
	for _, id := range actions.AddTagIDs {
		seen[id] = true
	}
	for _, id := range actions.RemoveTagIDs {
		if seen[id] {
			return MacroActions{}, &InvalidMacroError{"A macro cannot add and remove the same tag."}
		}
	}
	return actions, nil
}

func cleanMacroTags(ids []string, verb string) ([]string, error) {
	if len(ids) > MaxMacroTags {
		return nil, &InvalidMacroError{fmt.Sprintf("A macro can %s at most %d tags.", verb, MaxMacroTags)}
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if !macroIDPattern.MatchString(id) {
			return nil, &InvalidMacroError{"Tag ids are the ids from the workspace's tags."}
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// RenderMacro fills the variables an agent reads before sending. A missing value is removed with
// the space before it. Any other {{…}} stays as written so the agent sees it.
func RenderMacro(body string, context MacroContext) string {
	values := map[string]string{
		"customer.name":  strings.TrimSpace(context.CustomerName),
		"requester.name": strings.TrimSpace(context.CustomerName),
		"agent.name":     strings.TrimSpace(context.AgentName),
		"ticket.id":      strings.TrimSpace(context.TicketID),
	}
	return macroVariable.ReplaceAllStringFunc(body, func(match string) string {
		parts := macroVariable.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		if values[parts[2]] == "" {
			return ""
		}
		return parts[1] + values[parts[2]]
	})
}

// AgentSignatureName is the name {{agent.name}} stands for. An agent with no display name yet is
// named by the part of the email before the @, without a +tag.
func AgentSignatureName(displayName, email string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	local := email
	if at := strings.IndexByte(email, '@'); at >= 0 {
		local = email[:at]
	}
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	return strings.TrimSpace(local)
}
