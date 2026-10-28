package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestTheClientBuildHasOnlyMCPVersionAndHelp(t *testing.T) {
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{"version"}, env(nil), nil, &out, &errs); code != 0 || out.String() != "dev\n" {
		t.Errorf("version: %d %q", code, out.String())
	}
	out.Reset()
	if code := run(context.Background(), nil, env(nil), nil, &out, &errs); code != 0 || !strings.Contains(out.String(), "collie mcp") {
		t.Errorf("help: %d %q", code, out.String())
	}
	errs.Reset()
	if code := run(context.Background(), []string{"serve"}, env(nil), nil, &out, &errs); code != 2 || !strings.Contains(errs.String(), "Docker image") {
		t.Errorf("serve: %d %q", code, errs.String())
	}
	errs.Reset()
	if code := run(context.Background(), []string{"mcp"}, env(map[string]string{"COLLIEDESK_URL": "https://desk.example.com"}), strings.NewReader(""), &out, &errs); code != 2 || !strings.Contains(errs.String(), "COLLIEDESK_TOKEN") {
		t.Errorf("mcp without a token: %d %q", code, errs.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"mcp"}, env(map[string]string{"COLLIEDESK_URL": "https://desk.example.com", "COLLIEDESK_TOKEN": "cdk_agent_x"}), strings.NewReader(""), &out, &errs); code != 0 || out.Len() != 0 {
		t.Errorf("mcp with an empty stdin ends at once and writes nothing: %d %q", code, out.String())
	}
}

// The client build is about 6 MB because it leaves the web app and the database out; importing
// either brings back the 42 MB server.
func TestTheClientBuildLeavesTheServerOut(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	deps, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range strings.Fields(string(deps)) {
		if strings.HasPrefix(dep, "github.com/") && dep != "github.com/colliedesk/colliedesk/server/internal/mcpbridge" && dep != "github.com/colliedesk/colliedesk/server/cmd/collie-client" {
			t.Errorf("the client build imports %s", dep)
		}
	}
}
