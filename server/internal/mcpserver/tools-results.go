package mcpserver

import "encoding/json"

func resultsGet(c *call, _ map[string]any) (any, error) {
	var page json.RawMessage
	if err := c.get("/api/v1/results", nil, &page); err != nil {
		return nil, err
	}
	return map[string]any{"results": page}, nil
}
