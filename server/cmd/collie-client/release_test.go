package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The Claude Desktop extension runs this collie with the token only in its environment: Claude
// Desktop shows a command's arguments in its settings and logs, and ps shows them to every user.
func TestTheExtensionPassesTheTokenOnlyThroughTheEnvironment(t *testing.T) {
	raw, err := os.ReadFile("mcpb-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ManifestVersion string `json:"manifest_version"`
		Name            string `json:"name"`
		Version         string `json:"version"`
		Description     string `json:"description"`
		Author          struct {
			Name string `json:"name"`
		} `json:"author"`
		Server struct {
			Type       string `json:"type"`
			EntryPoint string `json:"entry_point"`
			MCPConfig  struct {
				Command           string                       `json:"command"`
				Args              []string                     `json:"args"`
				Env               map[string]string            `json:"env"`
				PlatformOverrides map[string]map[string]string `json:"platform_overrides"`
			} `json:"mcp_config"`
		} `json:"server"`
		Compatibility struct {
			Platforms []string `json:"platforms"`
		} `json:"compatibility"`
		UserConfig map[string]struct {
			Type      string `json:"type"`
			Sensitive bool   `json:"sensitive"`
			Required  bool   `json:"required"`
		} `json:"user_config"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ManifestVersion == "" || manifest.Name != "colliedesk" || manifest.Version != "0.0.0" || manifest.Description == "" || manifest.Author.Name == "" {
		t.Errorf("required fields (the release stamps version over 0.0.0): %+v", manifest)
	}
	server := manifest.Server
	if server.Type != "binary" || server.EntryPoint != "server/collie" || server.MCPConfig.Command != "${__dirname}/server/collie" ||
		server.MCPConfig.PlatformOverrides["win32"]["command"] != "${__dirname}/server/collie.exe" {
		t.Errorf("server: %+v", server)
	}
	if strings.Join(server.MCPConfig.Args, " ") != "mcp --url ${user_config.url}" {
		t.Errorf("args %q must hold the address and never the token", server.MCPConfig.Args)
	}
	if len(server.MCPConfig.Env) != 1 || server.MCPConfig.Env["COLLIEDESK_TOKEN"] != "${user_config.token}" {
		t.Errorf("env %v must carry the token, and only it", server.MCPConfig.Env)
	}
	if token := manifest.UserConfig["token"]; !token.Sensitive || !token.Required || token.Type != "string" {
		t.Errorf("the token must be a required sensitive field: %+v", token)
	}
	if url := manifest.UserConfig["url"]; url.Sensitive || !url.Required {
		t.Errorf("url: %+v", url)
	}
	if strings.Join(manifest.Compatibility.Platforms, ",") != "darwin,win32" {
		t.Errorf("the bundle carries macOS and Windows binaries only: %v", manifest.Compatibility.Platforms)
	}
}

// Asset names follow uname (and PROCESSOR_ARCHITECTURE on Windows), so the install command on a
// guide picks its file without a lookup table.
func TestClientAssetsAreNamedTheWayTheSystemNamesItself(t *testing.T) {
	file, err := os.Open("../../../.github/scripts/client-assets.txt")
	if err != nil {
		t.Skipf("the repository is not next to this module: %v", err)
	}
	defer file.Close()
	systems := map[string]string{"darwin": "Darwin", "linux": "Linux", "windows": "Windows"}
	machines := map[string]map[string]string{
		"darwin":  {"arm64": "arm64", "amd64": "x86_64"},
		"linux":   {"arm64": "aarch64", "amd64": "x86_64"},
		"windows": {"arm64": "ARM64", "amd64": "AMD64"},
	}
	built := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 3 {
			t.Fatalf("line %q: want GOOS GOARCH asset", scanner.Text())
		}
		goos, goarch, asset := fields[0], fields[1], fields[2]
		switch goos {
		case "bundle":
			if asset != "colliedesk.mcpb" {
				t.Errorf("bundle %q", asset)
			}
			continue
		case "checksums":
			if asset != "SHA256SUMS" {
				t.Errorf("checksums %q", asset)
			}
			continue
		}
		extension := ".tar.gz"
		if goos == "windows" {
			extension = ".zip"
		}
		if want := "collie_" + systems[goos] + "_" + machines[goos][goarch] + extension; asset != want {
			t.Errorf("%s/%s is %s, want %s", goos, goarch, asset, want)
		}
		built[goos+"/"+goarch] = true
	}
	// The extension is made of these three.
	for _, needed := range []string{"darwin/arm64", "darwin/amd64", "windows/amd64"} {
		if !built[needed] {
			t.Errorf("%s is not built", needed)
		}
	}
}
