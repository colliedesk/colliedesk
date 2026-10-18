package httpapi

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDisplayNameRunes bounds a display name after trimming; it is a label in lists, not a signature.
const maxDisplayNameRunes = 80

// normalizeDisplayName trims raw and reports whether the result may be stored. An empty result is
// valid: it clears the name, and the email address is shown again. Control characters and the
// text-direction overrides are refused, so a name cannot break a line or reorder the text around it.
func normalizeDisplayName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxDisplayNameRunes {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') {
			return "", false
		}
	}
	return name, true
}

const displayNameInvalid = "display_name must be a string of at most 80 characters without control characters; send an empty string to clear it."

// updateAgentDisplayName sets the display name of the agent the session or token belongs to.
func (a *API) updateAgentDisplayName(response http.ResponseWriter, request *http.Request) {
	var input struct {
		DisplayName *string `json:"display_name"`
	}
	if err := decodeJSON(request, &input); err != nil || input.DisplayName == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", displayNameInvalid)
		return
	}
	name, ok := normalizeDisplayName(*input.DisplayName)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", displayNameInvalid)
		return
	}
	identity := principal(request)
	agent, err := a.store.UpdateAgentDisplayName(request.Context(), identity.Workspace.ID, identity.Agent.ID, name)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	agent.Role = identity.Agent.Role
	writeJSON(response, http.StatusOK, agent)
}
