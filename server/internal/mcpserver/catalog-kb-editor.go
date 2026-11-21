package mcpserver

import "net/http"

func myKBEditorSet(c *call, arguments map[string]any) (any, error) {
	var out map[string]any
	if err := c.send(http.MethodPut, "/api/v1/agents/me/kb-editor", map[string]any{"editor": arguments["editor"]}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func myKBEditorSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_kb_editor_set",
		Title: "Choose your own article editor",
		Description: `Choose the editor the team member this token acts as writes knowledge base articles in, in the web app: visual (formatted text, ` +
			`the default for someone who never chose) or markdown (Write, Split and Preview). Both edit the same Markdown, so nothing in an article changes. ` +
			`Other team members keep their own choice. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/agents/me/kb-editor"},
		Input:      `{"type":"object","additionalProperties":false,"required":["editor"],"properties":{"editor":{"type":"string","enum":["visual","markdown"]}}}`,
		Output:     `{"type":"object","required":["editor"],"properties":{"editor":{"type":"string","enum":["visual","markdown"]}}}`,
		run:        myKBEditorSet,
	}
}
