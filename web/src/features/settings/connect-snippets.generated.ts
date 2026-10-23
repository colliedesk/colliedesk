// Generated from server/internal/mcpserver/docgen by `make docs-mcp`; do not edit.
// The host guides in docs/user/connect-*.md show the same commands.

export const exampleOrigin = "https://desk.example.com";
export const mcpServerName = "colliedesk";
export const tokenEnv = "COLLIEDESK_TOKEN";
export const tokenPrefix = "cdk_agent_";

export const connectHosts = [
  { id: "claude-code", name: "Claude Code", guide: "https://colliedesk.com/docs/connect-claude-code" },
  { id: "codex", name: "Codex", guide: "https://colliedesk.com/docs/connect-codex" },
  { id: "cursor", name: "Cursor", guide: "https://colliedesk.com/docs/connect-cursor" },
  { id: "vscode", name: "VS Code", guide: "https://colliedesk.com/docs/connect-vscode" },
  { id: "claude-desktop", name: "Claude Desktop", guide: "https://colliedesk.com/docs/connect-claude-desktop" },
] as const;

export type ConnectHostID = (typeof connectHosts)[number]['id'];

export const connectSnippets = {
  claudeCodeCLI: "claude mcp add --transport http colliedesk https://desk.example.com/mcp --header 'Authorization: Bearer ${COLLIEDESK_TOKEN}'",
  claudeCodePlugin: "claude plugin install colliedesk --marketplace colliedesk/colliedesk --config url=https://desk.example.com/mcp",
  claudeCodeToken: "/plugin configure colliedesk@colliedesk",
  codex: "codex mcp add colliedesk --url https://desk.example.com/mcp --bearer-token-env-var COLLIEDESK_TOKEN",
  collieTokenFile: "(umask 077 \u0026\u0026 pbpaste \u003e ~/.colliedesk-token)",
  collieTokenFileWindows: "Get-Clipboard | Set-Content -NoNewline \"$HOME\\.colliedesk-token\"",
  collieUnix: "mkdir -p ~/.local/bin\ncurl -fsSL https://github.com/colliedesk/colliedesk/releases/latest/download/collie_$(uname -s)_$(uname -m).tar.gz | tar -xz -C ~/.local/bin collie\necho \"$HOME/.local/bin/collie\"",
  collieWindows: "$dir = \"$env:LOCALAPPDATA\\Programs\\collie\"\nNew-Item -ItemType Directory -Force $dir | Out-Null\nInvoke-WebRequest \"https://github.com/colliedesk/colliedesk/releases/latest/download/collie_Windows_$env:PROCESSOR_ARCHITECTURE.zip\" -OutFile \"$env:TEMP\\collie.zip\"\nExpand-Archive -Force \"$env:TEMP\\collie.zip\" $dir\n\"$dir\\collie.exe\"",
  cursor: "{\n  \"mcpServers\": {\n    \"colliedesk\": {\n      \"url\": \"https://desk.example.com/mcp\",\n      \"headers\": {\n        \"Authorization\": \"Bearer ${env:COLLIEDESK_TOKEN}\"\n      }\n    }\n  }\n}",
  desktopBundle: "https://github.com/colliedesk/colliedesk/releases/latest/download/colliedesk.mcpb",
  stdio: "{\n  \"mcpServers\": {\n    \"colliedesk\": {\n      \"command\": \"/Users/you/.local/bin/collie\",\n      \"args\": [\n        \"mcp\",\n        \"--url\",\n        \"https://desk.example.com\",\n        \"--token-file\",\n        \"/Users/you/.colliedesk-token\"\n      ]\n    }\n  }\n}",
  tokenEnv: "export COLLIEDESK_TOKEN=\u003cyour token\u003e",
  vscode: "{\n  \"inputs\": [\n    {\n      \"type\": \"promptString\",\n      \"id\": \"colliedesk-token\",\n      \"description\": \"Colliedesk agent token\",\n      \"password\": true\n    }\n  ],\n  \"servers\": {\n    \"colliedesk\": {\n      \"type\": \"http\",\n      \"url\": \"https://desk.example.com/mcp\",\n      \"headers\": {\n        \"Authorization\": \"Bearer ${input:colliedesk-token}\"\n      }\n    }\n  }\n}",
} as const;
