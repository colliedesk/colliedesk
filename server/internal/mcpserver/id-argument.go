package mcpserver

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// renameIDArgument reads id as the one required *_id argument of a tool that takes no id of its own:
// agents often call colliedesk_conversation_get with id instead of conversation_id. The schema the
// client sees is unchanged; only the arguments are. Both names with different values are refused.
func renameIDArgument(schema *jsonschema.Schema, arguments map[string]any) error {
	id, given := arguments["id"]
	if !given || schema == nil {
		return nil
	}
	if _, takesID := schema.Properties["id"]; takesID {
		return nil
	}
	target := ""
	for _, name := range schema.Required {
		if strings.HasSuffix(name, "_id") {
			if target != "" {
				return nil
			}
			target = name
		}
	}
	if target == "" {
		return nil
	}
	if other, both := arguments[target]; both && !reflect.DeepEqual(other, id) {
		return fmt.Errorf("id and %s are the same argument and differ here. Give only %s", target, target)
	}
	delete(arguments, "id")
	arguments[target] = id
	return nil
}
