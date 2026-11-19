package mcpserver

import (
	"maps"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// argumentHint says how to fix arguments the input schema refused: the argument meant by a wrong
// name, what goes in a missing one, and, when a wrong name has no close match, every argument the
// tool takes. A model fixes its next call from this text instead of guessing again.
func argumentHint(tool string, schema *jsonschema.Schema, arguments map[string]any) string {
	if schema == nil || len(schema.Properties) == 0 {
		return ""
	}
	var hints []string
	unmatched := false
	for _, name := range slices.Sorted(maps.Keys(arguments)) {
		if _, known := schema.Properties[name]; known {
			continue
		}
		if near := nearArguments(name, schema.Properties); len(near) > 0 {
			hints = append(hints, "Did you mean "+strings.Join(near, " or ")+" instead of "+name+"?")
		} else {
			unmatched = true
		}
	}
	for _, name := range schema.Required {
		if _, given := arguments[name]; given {
			continue
		}
		if property := schema.Properties[name]; property != nil && property.Description != "" {
			hints = append(hints, name+": "+property.Description)
		}
	}
	if unmatched {
		hints = append(hints, tool+" takes: "+strings.Join(slices.Sorted(maps.Keys(schema.Properties)), ", ")+".")
	}
	return strings.Join(hints, " ")
}

// nearArguments are the arguments a wrong name most likely meant: one is the start of the other
// (query for q, assignee_id for assignee) or the wrong name ends one of them after an underscore
// (id for conversation_id, tags for add_tags). More than three is no hint.
func nearArguments[V any](name string, properties map[string]V) []string {
	var near []string
	for property := range properties {
		if strings.HasPrefix(name, property) || strings.HasPrefix(property, name) || strings.HasSuffix(property, "_"+name) {
			near = append(near, property)
		}
	}
	if len(near) > 3 {
		return nil
	}
	slices.Sort(near)
	return near
}
