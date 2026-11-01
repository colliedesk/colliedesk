package httpapi

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/ai"
)

// settingsObjectFields lists, for a settings route, each field that takes a JSON object and the keys
// that object may hold. Any other field takes a plain value or a list of plain values, so no object
// can carry a key past the check.
var settingsObjectFields = map[string]map[string][]string{
	"PUT /api/v1/help-center/theme": {"colors": themeColorFields},
}

// settingsCheckedFields lists, for a settings route, each field whose object its own parser checks (any key
// it does not know is refused), as the call does when it runs: such a field holds lists and objects of its own.
var settingsCheckedFields = map[string]map[string]func(json.RawMessage) error{
	"PUT " + channelAIRoute: {"options": ai.CheckIntakeOptions},
}

// settingsRequiredFields lists, for a settings route, the fields its call refuses to run without; a
// request without one would fail on approval, so it is not filed.
var settingsRequiredFields = map[string]map[string]string{
	"PUT /api/v1/help-center/theme": {"custom_css": customCSSRequired},
}

// settingsBodyProblem says why body cannot be filed for a settings route: it is not a JSON object, it
// lacks a field the route requires, or it has a field (at any depth) the route does not take. It
// returns "" when the body may be filed.
func settingsBodyProblem(route Route, body json.RawMessage) string {
	key := route.Method + " " + route.Pattern
	allowed, ok := settingsFields[key]
	if !ok || len(body) == 0 && settingsRequiredFields[key] == nil {
		return ""
	}
	fields, isObject := jsonObject(body)
	if !isObject || fields == nil {
		return "The body of this call must be a JSON object. Nothing was filed."
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(allowed, name) {
			return unexpectedSettingsField(name)
		}
		if check, checked := settingsCheckedFields[key][name]; checked {
			if err := check(fields[name]); err != nil {
				return strings.TrimSuffix(err.Error(), ".") + ". Nothing was filed."
			}
			continue
		}
		keys, takesObject := settingsObjectFields[key][name]
		if !takesObject {
			if !plainJSONValue(fields[name], true) {
				return "The field " + name + " takes a plain value, not an object. Nothing was filed."
			}
			continue
		}
		inner, isObject := jsonObject(fields[name])
		if !isObject {
			if string(bytes.TrimSpace(fields[name])) == "null" {
				continue
			}
			return "The field " + name + " takes an object of " + strings.Join(keys, ", ") + ". Nothing was filed."
		}
		for _, innerName := range slices.Sorted(maps.Keys(inner)) {
			if !slices.Contains(keys, innerName) {
				return unexpectedSettingsField(name + "." + innerName)
			}
			if !plainJSONValue(inner[innerName], false) {
				return "The field " + name + "." + innerName + " takes a plain value, not an object or list. Nothing was filed."
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(settingsRequiredFields[key])) {
		if _, sent := fields[name]; !sent {
			return settingsRequiredFields[key][name] + " Nothing was filed."
		}
	}
	return ""
}

func unexpectedSettingsField(name string) string {
	return "This call does not take the field " + name + ". Nothing was filed; send only the fields the route takes."
}

// jsonObject decodes value when it is a JSON object.
func jsonObject(value json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		return nil, false
	}
	return fields, true
}

// plainJSONValue is true for a string, number, boolean or null and, when lists is set, for a list of
// those.
func plainJSONValue(value json.RawMessage, lists bool) bool {
	trimmed := bytes.TrimSpace(value)
	switch {
	case len(trimmed) == 0:
		return false
	case trimmed[0] == '{':
		return false
	case trimmed[0] == '[':
		var items []json.RawMessage
		if !lists || json.Unmarshal(trimmed, &items) != nil {
			return false
		}
		for _, item := range items {
			if !plainJSONValue(item, false) {
				return false
			}
		}
	}
	return true
}
