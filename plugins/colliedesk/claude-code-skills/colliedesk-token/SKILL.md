---
name: colliedesk-token
description: Replace the agent token the Colliedesk plugin sends to your Colliedesk. Run it yourself as /colliedesk-token <token> when the Colliedesk MCP server fails with 401 or the old token was revoked.
argument-hint: <token>
disable-model-invocation: true
allowed-tools: Bash(sh "${CLAUDE_PLUGIN_ROOT}/scripts/colliedesk-token.sh" *)
---

# Replace the Colliedesk token

Result of checking and saving the token:

```!
sh "${CLAUDE_PLUGIN_ROOT}/scripts/colliedesk-token.sh" '${user_config.url}' <<'COLLIEDESK_TOKEN'
$ARGUMENTS
COLLIEDESK_TOKEN
```

Tell the person the result above in their language, in one or two sentences, and nothing more.
Do not run any other command, do not try another way to save a token, and never repeat the token.
