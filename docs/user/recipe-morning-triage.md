# Recipe: morning triage with an AI agent

Every weekday morning an AI agent sorts the open conversations nobody owns: tags, priority and
owner, with a private note on each that says what the customer wants. It never writes to a
customer. Your team starts the day with a sorted queue and a list of what the agent was unsure of.

This recipe runs [Claude Code](connect-claude-code.md) from cron on a computer that stays on. The
same setup with [Codex](connect-codex.md) is at the end.

## What the agent may do

Give the agent its own token, with only the permissions triage needs. Then a mistake in a prompt
cannot reach a customer.

1. Open **Settings → AI & agents → External AI agents** and choose **Create token**.
2. Name it **Morning triage**.
3. Under **Access**, choose **Pick permissions (fixed)** and tick only:
   `conversations:read`, `conversations:update`, `messages:read`, `messages:note`, `tags:read`,
   `tags:create`, `contacts:read`, `saved_views:read` and `team:read`.
   Leave **Send to customers** (`messages:send`) off.
4. Choose **Create token** and copy the token. It starts with `cdk_agent_` and is shown once.

Every note the agent writes shows "by <your name> via Morning triage". A call outside these
permissions is refused with the permission it lacks, and the agent reports it in its list.
More: [Agent tokens](sessions.md#agent-tokens) and [Roles and permissions](roles-and-permissions.md).

## Set up the folder

1. Make a folder for the job and put the token in a file only you can read:

   ```sh
   mkdir -p ~/colliedesk-triage/runs ~/.config/colliedesk
   cd ~/colliedesk-triage
   (umask 077; cat > ~/.config/colliedesk/triage-token)   # paste the token, then Ctrl-D
   ```

2. Copy the `colliedesk-triage` skill into the folder, so the agent knows the routine and its
   limits ([Agent skills](agent-skills.md)):

   ```sh
   git clone --depth 1 https://github.com/colliedesk/colliedesk.git /tmp/colliedesk
   mkdir -p .claude/skills
   cp -R /tmp/colliedesk/plugins/colliedesk/skills/colliedesk-triage .claude/skills/
   ```

3. Save this as `mcp.json` in the folder. On your own server, put its address in place of
   `app.colliedesk.com`. Claude Code reads the token from the variable when it starts; the file
   never holds it.

   ```json
   {"mcpServers": {"colliedesk": {
     "type": "http",
     "url": "https://app.colliedesk.com/mcp",
     "headers": {"Authorization": "Bearer ${COLLIEDESK_MCP_TOKEN}"}
   }}}
   ```

4. Save this as `run.sh` and make it executable (`chmod +x run.sh`). `--strict-mcp-config` loads
   only this server, so the Colliedesk plugin, which acts with all your permissions, stays out.
   `--allowedTools` lets the agent call Colliedesk tools and the skill, and nothing else.

   ```sh
   #!/bin/sh
   set -eu
   cd "$(dirname "$0")"
   COLLIEDESK_MCP_TOKEN=$(cat "$HOME/.config/colliedesk/triage-token")
   export COLLIEDESK_MCP_TOKEN
   claude -p "Use the colliedesk-triage skill: triage the open conversations nobody owns." \
     --mcp-config mcp.json --strict-mcp-config \
     --allowedTools "mcp__colliedesk__*" "Skill" \
     --output-format json > "runs/$(date +%Y-%m-%d-%H%M).json"
   ```

5. Run it once by hand: `./run.sh`. Claude Code must be signed in on this computer (run `claude`
   once) or have an API key.

## Check the run

```sh
jq -r '.result' runs/*.json | tail -40
jq '.permission_denials' runs/*.json
```

`result` ends with the skill's list: each conversation, what changed (tags, priority, owner, the
note) and what the agent was unsure of. `permission_denials` is `[]` when Claude Code refused no
call. In Colliedesk, the conversations it sorted have an owner and a private note from
**Morning triage**. Read the list the first few mornings; correct a tag or an owner like any
teammate's choice, and tell the agent in the prompt what to do differently.

## Run it every weekday

Add a line with `crontab -e`. Cron uses the computer's timezone and does not read your shell
profile, so give the full path to `claude` (`command -v claude`) in `run.sh` if cron cannot find it.

```
30 7 * * 1-5 $HOME/colliedesk-triage/run.sh >> $HOME/colliedesk-triage/cron.log 2>&1
```

To stop it, delete the line. To stop the agent at once, revoke the **Morning triage** token in
**Settings → AI & agents → External AI agents**.

## With Codex

Put the token in `COLLIEDESK_TOKEN` (read it from the same file in your script) and add this to
`~/.codex/config.toml`. `approve` lets the run call Colliedesk tools without a person there; the
token still limits what they can do, and it applies to every Codex session on this computer that
uses this server. `exclude` keeps the token out of shell commands Codex runs.

```toml
[mcp_servers.colliedesk]
url = "https://app.colliedesk.com/mcp"
bearer_token_env_var = "COLLIEDESK_TOKEN"
required = true
default_tools_approval_mode = "approve"

[shell_environment_policy]
exclude = ["COLLIEDESK_TOKEN"]
```

Copy the skill into `.agents/skills/` in the folder instead of `.claude/skills/`, then run:

```sh
codex exec --sandbox read-only --ephemeral --json -o last.txt \
  "Use the colliedesk-triage skill: triage the open conversations nobody owns." > run.jsonl
jq -e 'select(.item.type? == "mcp_tool_call" and .item.status == "failed")' run.jsonl && echo "a call failed"
```

`last.txt` holds the list. `codex exec` exits 0 even when a call was refused, so check `run.jsonl`.

## Try it

No workspace yet? [Start a free trial of Colliedesk Cloud](https://app.colliedesk.com/sign-in?intent=trial&utm_source=colliedesk.com&utm_medium=docs&utm_campaign=cookbook-morning-triage),
forward your support mail to it ([Email channel](email-channel.md)) and run the recipe on the first
morning's queue.
