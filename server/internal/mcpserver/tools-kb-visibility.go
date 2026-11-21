package mcpserver

// kbVisibilityOut is who an article or a collection is for, as set on it and as it applies; the API
// and the tools answer it with the same names.
type kbVisibilityOut struct {
	Visibility          string `json:"visibility"`
	EffectiveVisibility string `json:"effective_visibility"`
}

// kbVisibilityFromOut is the collection that keeps an article more restricted than its own visibility.
type kbVisibilityFromOut struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}
