package httpapi

import "net/http"

// setKBEditor records the article editor the caller writes in; GET /auth/me returns it as kb_editor.
func (a *API) setKBEditor(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Editor *string `json:"editor"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Editor == nil || (*input.Editor != "visual" && *input.Editor != "markdown") {
		writeError(response, http.StatusBadRequest, "invalid_request", `Send editor: "visual" or "markdown".`)
		return
	}
	who := principal(request)
	if err := a.store.SetKBEditor(request.Context(), who.Workspace.ID, who.Agent.ID, *input.Editor); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"editor": *input.Editor})
}
