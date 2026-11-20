package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// sharedShapeBytes is the smallest output shape, as compact JSON, that the tool list spells out in one
// tool only. Below it the reference costs about as much as the shape.
const sharedShapeBytes = 500

// advertisedOutputs returns the output schema each tool carries in the tool list. A shape that several
// tools return (an article, a fix attempt, an import) is spelled out in one of them, a read tool where
// one returns it; the others carry its type and "Same shape as `field` in tool's output.", so a client
// that loads every tool pays for the shape once. Results are still checked against the full schema
// (Spec.Output); the advertised one only drops detail, so it accepts every value the full one accepts.
func advertisedOutputs(specs []Spec) map[string]json.RawMessage {
	trees := make(map[string]any, len(specs))
	rank := make(map[string]int, len(specs))
	for _, spec := range specs {
		trees[spec.Name] = decodeSchema(spec.Name, spec.Output)
	}
	ordered := append([]Spec(nil), specs...)
	sort.SliceStable(ordered, func(i, j int) bool { return ownsBefore(ordered[i], ordered[j]) })
	for i, spec := range ordered {
		rank[spec.Name] = i
	}

	byShape := map[string][]place{}
	for _, spec := range specs {
		collectPlaces(spec.Name, &trees, byShape)
	}
	shapes := make([]string, 0, len(byShape))
	for shape, places := range byShape {
		if len(shape) >= sharedShapeBytes && spansTools(places) {
			shapes = append(shapes, shape)
		}
	}
	sort.Slice(shapes, func(i, j int) bool {
		if len(shapes[i]) != len(shapes[j]) {
			return len(shapes[i]) > len(shapes[j])
		}
		return shapes[i] < shapes[j]
	})

	// Larger shapes go first, so a place a reference replaced is gone before its parts come up. A part of
	// the owner's copy may itself name a smaller shape's owner: every reference points to a smaller
	// shape, so following them always ends.
	stubbed := map[string][]string{}
	for _, shape := range shapes {
		live := []place{}
		for _, p := range byShape[shape] {
			if !under(stubbed[p.tool], p.pointer) {
				live = append(live, p)
			}
		}
		if !spansTools(live) {
			continue
		}
		owner := live[0]
		for _, p := range live[1:] {
			if rank[p.tool] < rank[owner.tool] {
				owner = p
			}
		}
		for _, p := range live {
			if p.tool == owner.tool {
				continue
			}
			p.replace(stub(p.node, owner))
			stubbed[p.tool] = append(stubbed[p.tool], p.pointer)
		}
	}

	outputs := make(map[string]json.RawMessage, len(specs))
	for _, spec := range specs {
		if len(stubbed[spec.Name]) == 0 {
			outputs[spec.Name] = json.RawMessage(spec.Output)
			continue
		}
		outputs[spec.Name] = json.RawMessage(compact(trees[spec.Name]))
	}
	return outputs
}

// ownsBefore orders the tools that may spell out a shared shape: a tool that reads one thing, then any
// read tool, then the shorter name.
func ownsBefore(a, b Spec) bool {
	if ga, gb := strings.HasSuffix(a.Name, "_get"), strings.HasSuffix(b.Name, "_get"); ga != gb {
		return ga
	}
	if ra, rb := a.ReadOnly(), b.ReadOnly(); ra != rb {
		return ra
	}
	if len(a.Name) != len(b.Name) {
		return len(a.Name) < len(b.Name)
	}
	return a.Name < b.Name
}

// place is one schema object in one tool's output schema.
type place struct {
	tool    string
	pointer string // JSON pointer into the schema, for nesting
	field   string // what a reader calls it: article.translations[]
	node    map[string]any
	replace func(any)
}

func collectPlaces(tool string, trees *map[string]any, byShape map[string][]place) {
	var walk func(node any, pointer, field string, replace func(any))
	walk = func(node any, pointer, field string, replace func(any)) {
		schema, ok := node.(map[string]any)
		if !ok {
			return
		}
		shape := compact(schema)
		byShape[shape] = append(byShape[shape], place{tool: tool, pointer: pointer, field: field, node: schema, replace: replace})
		if properties, ok := schema["properties"].(map[string]any); ok {
			for _, name := range sortedKeys(properties) {
				child := name
				walk(properties[child], pointer+"/properties/"+child, join(field, child), func(v any) { properties[child] = v })
			}
		}
		walk(schema["items"], pointer+"/items", field+"[]", func(v any) { schema["items"] = v })
		walk(schema["additionalProperties"], pointer+"/additionalProperties", field+"{}", func(v any) { schema["additionalProperties"] = v })
		for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
			alternatives, _ := schema[keyword].([]any)
			for i := range alternatives {
				at := i
				walk(alternatives[at], fmt.Sprintf("%s/%s/%d", pointer, keyword, at), fmt.Sprintf("%s%s[%d]", field, keyword, at), func(v any) { alternatives[at] = v })
			}
		}
	}
	walk((*trees)[tool], "", "", func(v any) { (*trees)[tool] = v })
}

func stub(original map[string]any, owner place) map[string]any {
	where := owner.tool + "'s output."
	if owner.field != "" {
		where = "`" + owner.field + "` in " + where
	}
	text := "Same shape as " + where
	if described, _ := original["description"].(string); described != "" {
		text = described + " " + text
	}
	replaced := map[string]any{"description": text}
	if kind, ok := original["type"]; ok {
		replaced["type"] = kind
	}
	return replaced
}

func spansTools(places []place) bool {
	for _, p := range places[1:] {
		if p.tool != places[0].tool {
			return true
		}
	}
	return false
}

// under says the pointer is one of the prefixes or inside one.
func under(prefixes []string, pointer string) bool {
	for _, prefix := range prefixes {
		if pointer == prefix || strings.HasPrefix(pointer, prefix+"/") {
			return true
		}
	}
	return false
}

func join(field, name string) string {
	if field == "" {
		return name
	}
	return field + "." + name
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func decodeSchema(tool, raw string) any {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var schema any
	if err := decoder.Decode(&schema); err != nil {
		panic(fmt.Errorf("tool %s: output schema: %w", tool, err))
	}
	return schema
}

func compact(node any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(node); err != nil {
		panic(err)
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}
