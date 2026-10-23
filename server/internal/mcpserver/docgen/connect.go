package docgen

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/template"

	"github.com/colliedesk/colliedesk/server/internal/mcpbridge"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The values every host guide and the web app's Connect panel share. ExampleOrigin stands for the
// address of a person's Colliedesk: the guides show it, the web app puts its own origin in its place.
const (
	ExampleOrigin = "https://desk.example.com"
	ServerName    = "colliedesk"
	Releases      = "https://github.com/colliedesk/colliedesk/releases/latest/download/"
	Marketplace   = "colliedesk/colliedesk"
	PluginID      = "colliedesk@colliedesk"
)

// Hosts are the AI hosts with a guide, docs/user/connect-<ID>.md, in the order the guides list them.
var Hosts = []Host{
	{ID: "claude-code", Name: "Claude Code"},
	{ID: "codex", Name: "Codex"},
	{ID: "cursor", Name: "Cursor"},
	{ID: "vscode", Name: "VS Code"},
	{ID: "claude-desktop", Name: "Claude Desktop"},
}

type Host struct{ ID, Name string }

// Tier is one row of the tier table of a guide.
type Tier struct {
	Name     string
	Callable int // tools the tier calls directly; the rest file an agent request
}

// Data is what the guide templates read.
type Data struct {
	Origin, MCPURL, ServerName, TokenEnv, URLEnv, TokenPrefix string
	ProtocolVersions                                          []string
	Tools                                                     int
	Tiers                                                     []Tier
	Snippets                                                  map[string]string
	Assets                                                    map[string]string // client-assets.txt: "darwin/arm64", "bundle", "checksums" to file name
	Releases, Marketplace, PluginID                           string
}

// NewData reads the catalog and the asset list (the path of client-assets.txt).
func NewData(specs []mcpserver.Spec, assetList string) (Data, error) {
	assets, err := readAssets(assetList)
	if err != nil {
		return Data{}, err
	}
	data := Data{
		Origin: ExampleOrigin, MCPURL: ExampleOrigin + mcpserver.Path, ServerName: ServerName,
		TokenEnv: mcpbridge.TokenEnv, URLEnv: mcpbridge.URLEnv, TokenPrefix: mcpbridge.TokenPrefix,
		ProtocolVersions: mcp.SupportedProtocolVersions(), Tools: len(specs), Assets: assets,
		Releases: Releases, Marketplace: Marketplace, PluginID: PluginID,
	}
	rank := map[string]int{"any": 0, "read": 1, "draft": 2, "send": 3, "admin": 4}
	for _, tier := range []string{"read", "draft", "send", "admin"} {
		row := Tier{Name: tier}
		for _, spec := range specs {
			if rank[spec.Tier] <= rank[tier] {
				row.Callable++
			}
		}
		data.Tiers = append(data.Tiers, row)
	}
	data.Snippets = snippets(data)
	return data, nil
}

// snippets are the commands and files a person copies, keyed for the templates and the web app.
// The address in them is ExampleOrigin; the token is never in them, only its variable or prompt.
func snippets(d Data) map[string]string {
	jsonOf := func(value any) string {
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(value)
		return strings.TrimSuffix(out.String(), "\n")
	}
	bearerEnv := "Bearer ${" + d.TokenEnv + "}"
	return map[string]string{
		"claudeCodePlugin": "claude plugin install colliedesk --marketplace " + d.Marketplace + " --config url=" + d.MCPURL,
		"claudeCodeToken":  "/plugin configure " + d.PluginID,
		"claudeCodeCLI":    "claude mcp add --transport http " + d.ServerName + " " + d.MCPURL + " --header 'Authorization: " + bearerEnv + "'",
		"tokenEnv":         "export " + d.TokenEnv + "=<your token>",
		"codex":            "codex mcp add " + d.ServerName + " --url " + d.MCPURL + " --bearer-token-env-var " + d.TokenEnv,
		"cursor": jsonOf(map[string]any{"mcpServers": map[string]httpServer{
			d.ServerName: {URL: d.MCPURL, Headers: authorization{"Bearer ${env:" + d.TokenEnv + "}"}},
		}}),
		"vscode": jsonOf(struct {
			Inputs  []promptInput         `json:"inputs"`
			Servers map[string]httpServer `json:"servers"`
		}{
			[]promptInput{{"promptString", "colliedesk-token", "Colliedesk agent token", true}},
			map[string]httpServer{d.ServerName: {Type: "http", URL: d.MCPURL, Headers: authorization{"Bearer ${input:colliedesk-token}"}}},
		}),
		"desktopBundle": d.Releases + d.Assets["bundle"],
		"collieUnix": "mkdir -p ~/.local/bin\n" +
			"curl -fsSL " + d.Releases + "collie_$(uname -s)_$(uname -m).tar.gz | tar -xz -C ~/.local/bin collie\n" +
			"echo \"$HOME/.local/bin/collie\"",
		"collieWindows": "$dir = \"$env:LOCALAPPDATA\\Programs\\collie\"\n" +
			"New-Item -ItemType Directory -Force $dir | Out-Null\n" +
			"Invoke-WebRequest \"" + d.Releases + "collie_Windows_$env:PROCESSOR_ARCHITECTURE.zip\" -OutFile \"$env:TEMP\\collie.zip\"\n" +
			"Expand-Archive -Force \"$env:TEMP\\collie.zip\" $dir\n" +
			"\"$dir\\collie.exe\"",
		"collieTokenFile": "(umask 077 && pbpaste > ~/.colliedesk-token)",
		"stdio": jsonOf(map[string]any{"mcpServers": map[string]stdioServer{d.ServerName: {
			Command: "/Users/you/.local/bin/collie",
			Args:    []string{"mcp", "--url", d.Origin, "--token-file", "/Users/you/.colliedesk-token"},
		}}}),
	}
}

// The configuration shapes of the snippets, as structs so their keys keep a readable order.
type (
	authorization struct {
		Authorization string `json:"Authorization"`
	}
	httpServer struct {
		Type    string        `json:"type,omitempty"`
		URL     string        `json:"url"`
		Headers authorization `json:"headers"`
	}
	stdioServer struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	promptInput struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		Description string `json:"description"`
		Password    bool   `json:"password"`
	}
)

func readAssets(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	assets := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s: line %q is not GOOS GOARCH asset", path, scanner.Text())
		}
		key := fields[0] + "/" + fields[1]
		if fields[1] == "-" {
			key = fields[0]
		}
		assets[key] = fields[2]
	}
	return assets, scanner.Err()
}

//go:embed templates/*.md.tmpl
var templates embed.FS

// Guides renders each host guide, by file name (connect-<ID>.md).
func Guides(data Data) (map[string]string, error) {
	parsed, err := template.New("").Funcs(template.FuncMap{
		// indent puts n spaces before every line but the first, for a block inside a list item.
		"indent": func(n int, text string) string {
			return strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", n))
		},
	}).ParseFS(templates, "templates/*.md.tmpl")
	if err != nil {
		return nil, err
	}
	pages := map[string]string{}
	for _, host := range Hosts {
		var out bytes.Buffer
		if err := parsed.ExecuteTemplate(&out, "connect-"+host.ID+".md.tmpl", struct {
			Data
			Host  Host
			Hosts []Host
		}{data, host, Hosts}); err != nil {
			return nil, fmt.Errorf("%s: %w", host.ID, err)
		}
		pages["connect-"+host.ID+".md"] = out.String()
	}
	return pages, nil
}

// Versions is the protocol sentence of docs/user/mcp.md.
func Versions(data Data) string {
	quoted := make([]string, len(data.ProtocolVersions))
	for i, version := range data.ProtocolVersions {
		quoted[i] = "`" + version + "`"
	}
	return "- URL: `https://<your-colliedesk>" + mcpserver.Path + "`. It uses MCP Streamable HTTP without sessions, protocol\n" +
		"  versions " + strings.Join(quoted, ", ") + ".\n"
}

// HostLinks is the list of guides in docs/user/mcp.md.
func HostLinks() string {
	var b strings.Builder
	for _, host := range Hosts {
		fmt.Fprintf(&b, "- [%s](connect-%s.md)\n", host.Name, host.ID)
	}
	return b.String()
}

// TypeScript is web/src/features/settings/connect-snippets.generated.ts: the same snippets for the
// Connect panel of Settings, which replaces ExampleOrigin with its own origin.
func TypeScript(data Data) string {
	var b strings.Builder
	b.WriteString("// Generated from server/internal/mcpserver/docgen by `make docs-mcp`; do not edit.\n")
	b.WriteString("// The host guides in docs/user/connect-*.md show the same commands.\n\n")
	str := func(s string) string {
		encoded, _ := json.Marshal(s)
		return string(encoded)
	}
	fmt.Fprintf(&b, "export const exampleOrigin = %s;\n", str(data.Origin))
	fmt.Fprintf(&b, "export const mcpServerName = %s;\n", str(data.ServerName))
	fmt.Fprintf(&b, "export const tokenEnv = %s;\n", str(data.TokenEnv))
	fmt.Fprintf(&b, "export const tokenPrefix = %s;\n\n", str(data.TokenPrefix))
	b.WriteString("export const connectHosts = [\n")
	for _, host := range Hosts {
		fmt.Fprintf(&b, "  { id: %s, name: %s, guide: %s },\n", str(host.ID), str(host.Name), str("https://colliedesk.com/docs/connect-"+host.ID))
	}
	b.WriteString("] as const;\n\n")
	b.WriteString("export type ConnectHostID = (typeof connectHosts)[number]['id'];\n\n")
	keys := make([]string, 0, len(data.Snippets))
	for key := range data.Snippets {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	b.WriteString("export const connectSnippets = {\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "  %s: %s,\n", key, str(data.Snippets[key]))
	}
	b.WriteString("} as const;\n")
	return b.String()
}
