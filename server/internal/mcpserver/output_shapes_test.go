package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// A shape of 500 bytes or more that several tools return (an article, a fix attempt) is spelled out
// once in the tool list; every other tool names where it is. A client that loads every tool pays for
// it once.
const sharedShapeBytes = 500

var sameShape = regexp.MustCompile("Same shape as (?:`([^`]+)` in )?(colliedesk_[a-z_]+)'s output\\.$")

func TestNoOutputShapeIsSpelledOutInTwoTools(t *testing.T) {
	advertised := listedOutputs(t)
	where := map[string][]string{}
	for _, name := range sortedNames(advertised) {
		walkSchema(advertised[name], "", func(node map[string]any, field string) {
			if encoded := canonical(node); len(encoded) >= sharedShapeBytes {
				where[encoded] = appendOnce(where[encoded], name)
			}
		})
	}
	repeated := 0
	for encoded, tools := range where {
		if len(tools) > 1 {
			repeated++
			if repeated <= 5 {
				t.Errorf("a %d-byte output shape is spelled out in %d tools (%s): %.120s", len(encoded), len(tools), strings.Join(tools, ", "), encoded)
			}
		}
	}
	if repeated > 5 {
		t.Errorf("and %d more shapes spelled out in more than one tool", repeated-5)
	}
}

// Each "Same shape as" names a tool and a field whose advertised schema, following the references in it,
// is the full shape the tool returns, so a client can read it there.
func TestASharedOutputShapeNamesTheToolThatSpellsItOut(t *testing.T) {
	advertised := listedOutputs(t)
	full := map[string]any{}
	for _, spec := range mcpserver.Catalog() {
		full[spec.Name] = decode(t, spec.Output)
	}
	referred := 0
	for _, name := range sortedNames(advertised) {
		fields := fieldsOf(full[name])
		walkSchema(advertised[name], "", func(node map[string]any, field string) {
			text, _ := node["description"].(string)
			match := sameShape.FindStringSubmatch(text)
			if match == nil {
				if strings.Contains(text, "Same shape as ") {
					t.Errorf("%s %s: %q names a shared shape in a form a client cannot follow", name, field, text)
				}
				return
			}
			referred++
			original := fields[field]
			if original == nil {
				t.Errorf("%s: %q has no field %q in its full output schema", name, text, field)
				return
			}
			if canonical(node["type"]) != canonical(original["type"]) {
				t.Errorf("%s %s: advertised type %v, want the full schema's %v", name, field, node["type"], original["type"])
			}
			if strings.Contains(match[1], "Of[") {
				t.Errorf("%s %s: %q names one alternative of an anyOf, oneOf or allOf, which a reader cannot find by name", name, field, text)
			}
			owner, ok := advertised[match[2]]
			if !ok || match[2] == name {
				t.Errorf("%s %s names %q, which is not another listed tool", name, field, match[2])
				return
			}
			spelled := fieldsOf(owner)[match[1]]
			if spelled == nil || canonical(expand(t, spelled, advertised, 0)) != canonical(original) {
				t.Errorf("%s %s: %s's %q is not the full shape the tool returns\nwant %.200s\ngot  %.200s", name, field, match[2], match[1], canonical(original), canonical(expand(t, spelled, advertised, 0)))
			}
		})
	}
	if referred == 0 {
		t.Error("no tool refers to a shape another tool spells out")
	}
}

// expand replaces each "Same shape as" in a schema with the schema it names, as a client reads it.
func expand(t *testing.T, node any, advertised map[string]any, depth int) any {
	t.Helper()
	if depth > 20 {
		t.Fatal("references to shared output shapes go round in a circle")
	}
	switch value := node.(type) {
	case map[string]any:
		text, _ := value["description"].(string)
		if match := sameShape.FindStringSubmatch(text); match != nil {
			return expand(t, fieldsOf(advertised[match[2]])[match[1]], advertised, depth+1)
		}
		copied := make(map[string]any, len(value))
		for key, child := range value {
			copied[key] = expand(t, child, advertised, depth)
		}
		return copied
	case []any:
		copied := make([]any, len(value))
		for i, child := range value {
			copied[i] = expand(t, child, advertised, depth)
		}
		return copied
	}
	return node
}

func listedOutputs(t *testing.T) map[string]any {
	t.Helper()
	e := newEnv(t)
	w := e.newWorkspace("Shapes Desk", "admin@shapes.example.com")
	listed, err := e.connect(w.read, "").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]any{}
	for _, tool := range listed.Tools {
		encoded, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		outputs[tool.Name] = decode(t, string(encoded))
	}
	return outputs
}

// walkSchema visits every schema object in a JSON schema with its field path: property names joined by
// dots, [] for the items of an array, anyOf[0] for an alternative.
func walkSchema(node any, field string, visit func(map[string]any, string)) {
	schema, ok := node.(map[string]any)
	if !ok {
		return
	}
	visit(schema, field)
	join := func(name string) string {
		if field == "" {
			return name
		}
		return field + "." + name
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for name, child := range properties {
			walkSchema(child, join(name), visit)
		}
	}
	walkSchema(schema["items"], field+"[]", visit)
	walkSchema(schema["additionalProperties"], field+"{}", visit)
	for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
		alternatives, _ := schema[keyword].([]any)
		for i, child := range alternatives {
			walkSchema(child, fmt.Sprintf("%s%s[%d]", field, keyword, i), visit)
		}
	}
}

func fieldsOf(schema any) map[string]map[string]any {
	fields := map[string]map[string]any{}
	walkSchema(schema, "", func(node map[string]any, field string) { fields[field] = node })
	return fields
}

func canonical(node any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(node)
	return strings.TrimSuffix(buffer.String(), "\n")
}

func decode(t *testing.T, raw string) any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("schema does not decode: %v", err)
	}
	return value
}

func sortedNames(outputs map[string]any) []string {
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func appendOnce(list []string, name string) []string {
	if len(list) > 0 && list[len(list)-1] == name {
		return list
	}
	return append(list, name)
}
