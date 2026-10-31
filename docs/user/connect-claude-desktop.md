# Connect Claude Desktop

Connect [Claude Desktop](https://claude.ai/download) on macOS or Windows to Colliedesk with the
Colliedesk extension. Claude Desktop starts the small `collie` program that comes with it, which
carries Claude's requests to your Colliedesk; the token is kept by Claude Desktop, not in a file.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "Claude Desktop on my laptop"), pick a tier (`draft` is a good start: the agent
   can sort and annotate, and nothing reaches a customer) and choose **Create token**. See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Under **Connect your agent**, Settings shows these steps for Claude Desktop with your address already in.

## Connect

1. Download the extension: [colliedesk.mcpb](https://github.com/colliedesk/colliedesk/releases/latest/download/colliedesk.mcpb).
2. Open it (double-click), or drag it onto **Settings → Extensions** in Claude Desktop, and choose
   **Install**.
3. Fill in **Colliedesk address** (`https://desk.example.com`) and **Agent token**, and turn the extension on.

In a new chat, the tools menu lists Colliedesk.

The extension always holds the newest release of `collie`. Check a download against
[SHA256SUMS](https://github.com/colliedesk/colliedesk/releases/latest/download/SHA256SUMS), or prove it was built by
this repository with `gh attestation verify colliedesk.mcpb -R colliedesk/colliedesk`.

### Without the extension

Any host that starts a local MCP server (Claude Desktop's `claude_desktop_config.json` among them)
can run `collie mcp` itself. Install `collie` on macOS or Linux:

```sh
mkdir -p ~/.local/bin
curl -fsSL https://github.com/colliedesk/colliedesk/releases/latest/download/collie_$(uname -s)_$(uname -m).tar.gz | tar -xz -C ~/.local/bin collie
echo "$HOME/.local/bin/collie"
```

or on Windows, in PowerShell:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\collie"
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest "https://github.com/colliedesk/colliedesk/releases/latest/download/collie_Windows_$env:PROCESSOR_ARCHITECTURE.zip" -OutFile "$env:TEMP\collie.zip"
Expand-Archive -Force "$env:TEMP\collie.zip" $dir
"$dir\collie.exe"
```

The last line prints the full path of `collie`: the host needs it, because it does not read your
shell's `PATH`. Copy the token and save it in a file only you can read. On macOS:

```sh
(umask 077 && pbpaste > ~/.colliedesk-token)
```

On Linux, paste it into `~/.colliedesk-token` with an editor, then `chmod 600 ~/.colliedesk-token`.
On Windows, in PowerShell (Windows does not check who can read the file; your user folder is
private to you):

```powershell
Get-Clipboard | Set-Content -NoNewline "$HOME\.colliedesk-token"
```

Then give the host this server, with your paths and address (on Windows, the `collie.exe` path the
install printed and `C:\Users\you\.colliedesk-token`, with each `\` written twice in JSON):

```json
{
  "mcpServers": {
    "colliedesk": {
      "command": "/Users/you/.local/bin/collie",
      "args": [
        "mcp",
        "--url",
        "https://desk.example.com",
        "--token-file",
        "/Users/you/.colliedesk-token"
      ]
    }
  }
}
```

`collie mcp` takes the token from `--token-file` or the `COLLIEDESK_TOKEN` variable, never from its
arguments, which other programs can read. It refuses `http://` addresses except on this computer.
When something is wrong it answers the call with what to do (a revoked token, a wrong address, a
server that cannot be reached) and writes one line to the host's log. On Windows the token file's
permissions are not checked.

## A custom connector instead

Some Claude organizations see **Request headers** when an Owner adds a custom connector. That
connector reaches your Colliedesk from Anthropic's servers, so it must be reachable from the
internet, and everyone in the organization uses the one token the Owner entered: every action is
recorded as that token's person. Prefer the extension.

## Try it

Ask Claude Desktop: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, the token's tier and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What each tier can do

Colliedesk has 224 tools. A token calls the tools of its tier directly; any other call
becomes an [agent request](agent-requests.md) a person approves in **Requests**.

| Tier | Tools it calls directly |
|---|---|
| `read` | 107 of 224 |
| `draft` | 135 of 224 |
| `send` | 173 of 224 |
| `admin` | 224 of 224 |

Every tool and its tier: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

In Claude Desktop, **Settings → Extensions**, remove Colliedesk. Without the extension, delete
the `colliedesk` entry from the host's configuration and `~/.colliedesk-token`. Then revoke the token in **Settings → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Claude Code](connect-claude-code.md)
- [Codex](connect-codex.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

