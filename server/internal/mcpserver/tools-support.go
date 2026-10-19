package mcpserver

import (
	"net/http"
	"strings"
)

func problemReport(c *call, arguments map[string]any) (any, error) {
	if text, _ := arguments["description"].(string); strings.TrimSpace(text) == "" {
		return nil, &toolError{"The description is empty. Say what went wrong and what you expected in description."}
	}
	body := map[string]any{"description": arguments["description"]}
	if diagnostics, ok := arguments["diagnostics"]; ok {
		body["diagnostics"] = diagnostics
	}
	c.notFound = "This Colliedesk server does not receive problem reports. Open an issue at https://github.com/colliedesk/colliedesk/issues instead, and describe the problem there without customer names or email addresses."
	var created struct {
		Reference string `json:"reference"`
	}
	if err := c.send(http.MethodPost, "/api/v1/support/problem-reports", body, &created); err != nil {
		return nil, err
	}
	return map[string]any{"reference": created.Reference}, nil
}
