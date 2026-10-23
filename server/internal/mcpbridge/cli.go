package mcpbridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// Usage is the help of `collie mcp`.
const Usage = `Usage: collie mcp [--url https://desk.example.com] [--token-file PATH] [--timeout 60s]

Connects an MCP host that starts stdio servers (Claude Desktop) to Colliedesk.
The agent token comes from COLLIEDESK_TOKEN or --token-file, never from the command line.
The URL comes from --url or COLLIEDESK_URL: the site, or its /mcp endpoint.`

// TokenPrefix starts every agent token (contracts/openapi.yaml, createAgentToken).
const TokenPrefix = "cdk_agent_"

// Parse reads the flags and the environment. An error says what to fix; nothing has been read
// from stdin yet.
func Parse(args []string, getenv func(string) string) (Config, error) {
	flags := flag.NewFlagSet("collie mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	rawURL := flags.String("url", getenv("COLLIEDESK_URL"), "")
	tokenFile := flags.String("token-file", "", "")
	timeout := flags.Duration("timeout", 60*time.Second, "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, errors.New(Usage)
		}
		return Config{}, fmt.Errorf("%v\n%s", err, Usage)
	}
	if flags.NArg() > 0 {
		if strings.HasPrefix(flags.Arg(0), TokenPrefix) {
			return Config{}, errors.New("collie mcp takes no token on the command line, where other programs can read it: set COLLIEDESK_TOKEN or use --token-file")
		}
		return Config{}, fmt.Errorf("unexpected argument %q\n%s", flags.Arg(0), Usage)
	}
	if *timeout <= 0 {
		return Config{}, errors.New("--timeout must be more than 0, for example 60s")
	}
	endpoint, err := Endpoint(*rawURL)
	if err != nil {
		return Config{}, err
	}
	token := strings.TrimSpace(getenv("COLLIEDESK_TOKEN"))
	if *tokenFile != "" {
		if token, err = readTokenFile(*tokenFile); err != nil {
			return Config{}, err
		}
	}
	switch {
	case token == "":
		return Config{}, errors.New("no agent token: set COLLIEDESK_TOKEN or use --token-file. Create one in Colliedesk under Settings → Sessions")
	case !strings.HasPrefix(token, TokenPrefix):
		return Config{}, fmt.Errorf("the token does not start with %s, so it is not an agent token. Create one in Colliedesk under Settings → Sessions", TokenPrefix)
	}
	return Config{Endpoint: endpoint, Token: token, Timeout: *timeout}, nil
}

// Endpoint turns the site address or its /mcp URL into the /mcp URL. Plain http is refused except
// on this machine, because the token would cross the network readable.
func Endpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("no Colliedesk address: use --url https://desk.example.com or set COLLIEDESK_URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("%q is not a web address; use the address of your Colliedesk, for example https://desk.example.com", raw)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("use the address of your Colliedesk without a user name, query or #part, for example https://%s", parsed.Host)
	}
	if parsed.Scheme == "http" && !loopback(parsed.Hostname()) {
		return "", fmt.Errorf("http://%s would send the token unencrypted; use https://%s", parsed.Host, parsed.Host)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if !strings.HasSuffix(path, "/mcp") {
		path += "/mcp"
	}
	parsed.Path, parsed.RawPath = path, ""
	return parsed.String(), nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// readTokenFile reads a token file that only its owner can read. Windows has no such mode bits, so
// the check is Unix only (docs/user/connect-claude-desktop.md says so).
func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read the token file: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s can be read by other users of this computer; run chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the token file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Main is `collie mcp` for both builds of collie. It returns when stdin ends or ctx ends.
func Main(ctx context.Context, args []string, version string, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, err := Parse(args, os.Getenv)
	if err != nil {
		return err
	}
	cfg.Version, cfg.Stderr = version, stderr
	return Run(ctx, cfg, stdin, stdout)
}

func hostOf(endpoint string) string {
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return endpoint
}
