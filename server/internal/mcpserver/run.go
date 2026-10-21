package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handler runs one tool call with arguments that already match the input schema.
type handler func(c *call, arguments map[string]any) (any, error)

// register adds the tool to the server with the validation, the bearer hand-over and the error
// mapping that every tool shares.
func register(server *mcp.Server, spec Spec, rest *restClient) {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(spec.Input), &schema); err != nil {
		panic(fmt.Errorf("tool %s: input schema: %w", spec.Name, err))
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(fmt.Errorf("tool %s: resolve input schema: %w", spec.Name, err))
	}
	if spec.run == nil || !json.Valid([]byte(spec.Output)) {
		panic(fmt.Errorf("tool %s: no handler or a bad output schema", spec.Name))
	}
	annotations := &mcp.ToolAnnotations{Title: spec.Title, ReadOnlyHint: spec.ReadOnly, IdempotentHint: spec.Idempotent, OpenWorldHint: new(bool)}
	if !spec.ReadOnly {
		annotations.DestructiveHint = &spec.Destructive
	}
	tool := &mcp.Tool{Name: spec.Name, Title: spec.Title, Description: spec.Description, Annotations: annotations,
		InputSchema: json.RawMessage(spec.Input), OutputSchema: json.RawMessage(outputSchema(spec))}
	server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return execute(ctx, request, spec, resolved, rest), nil
	})
}

func execute(ctx context.Context, request *mcp.CallToolRequest, spec Spec, input *jsonschema.Resolved, rest *restClient) (result *mcp.CallToolResult) {
	c := &call{rest: rest, spec: spec}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = failure(failureText(c, fmt.Errorf("panic: %v", recovered)))
		}
	}()
	token, ok := bearerFrom(request)
	if !ok {
		return failure("This request carries no bearer token. Connect to Colliedesk with an agent token.")
	}
	arguments, err := parseArguments(request.Params.Arguments, input)
	if err != nil {
		return failure("Invalid arguments for " + spec.Name + ": " + err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c.ctx, c.token = ctx, token
	output, err := spec.run(c, arguments)
	var filed *agentRequested
	if errors.As(err, &filed) {
		output, err = requestedOutput(c, filed), nil
	}
	if err != nil {
		return failure(failureText(c, err))
	}
	image, isImage := output.(*imageResult)
	if isImage {
		output = image.Meta
	}
	data, err := json.Marshal(output)
	if err != nil {
		return failure(failureText(c, err))
	}
	content := []mcp.Content{&mcp.TextContent{Text: string(data)}}
	if isImage {
		content = append(content, &mcp.ImageContent{Data: image.Data, MIMEType: image.MIMEType})
	}
	return &mcp.CallToolResult{Content: content, StructuredContent: json.RawMessage(data)}
}

// imageResult is the answer of a tool that shows an image: the bytes go to the model as image content,
// Meta is the structured content that matches the tool's output schema.
type imageResult struct {
	Data     []byte
	MIMEType string
	Meta     any
}

func failure(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

func bearerFrom(request *mcp.CallToolRequest) (string, bool) {
	extra := request.GetExtra()
	if extra == nil || extra.Header == nil {
		return "", false
	}
	return bearerToken(&http.Request{Header: extra.Header})
}

// parseArguments decodes the arguments object and checks it against the tool's input schema.
func parseArguments(raw json.RawMessage, schema *jsonschema.Resolved) (map[string]any, error) {
	arguments := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, fmt.Errorf("the arguments must be a JSON object")
		}
	}
	if err := schema.Validate(arguments); err != nil {
		return nil, fmt.Errorf("%s", argumentProblem.Replace(err.Error()))
	}
	return arguments, nil
}

// argumentProblem drops the schema-path noise of a validation error, leaving the argument name and the reason.
var argumentProblem = strings.NewReplacer("validating root: ", "", "validating /properties/", "")

// decodeArguments copies validated arguments into the typed struct a handler reads.
func decodeArguments[T any](arguments map[string]any) (T, error) {
	var typed T
	encoded, err := json.Marshal(arguments)
	if err == nil {
		err = json.Unmarshal(encoded, &typed)
	}
	return typed, err
}
