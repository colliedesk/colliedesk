import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { ExternalLinkIcon } from '../../ui/icons';
import { AIAgentsSections, agentTokensShown } from '../settings/ai-agents-sections';
import { CopyButton } from '../settings/copy-button';
import { useMCPAddress } from '../settings/use-mcp-address';
import { agentStepMessages as m } from './setup-flow-messages';
import { useSetupFollows } from './use-setup-follows';
import './setup-agent-step.css';

// Where Claude lists Colliedesk Cloud as a connector: Connect there signs in with OAuth, as a custom connector does.
export const claudeDirectoryURL = 'https://claude.ai/directory/connectors/colliedesk';
// Where the agent tokens are listed, in Settings → AI & agents.
const tokensAddress = '/settings/ai?tab=external#agent-tokens-heading';

// AgentStep connects an AI app without a key first: on Colliedesk Cloud, Add to Claude opens Claude's directory;
// any app (and a server of its own) adds this server's MCP address and signs in with OAuth. An agent token, the
// Settings card with its steps per app, is the second choice. Connected (by anyone on the team), it says so and
// links to where this person sees their own connections.
export function AgentStep({ me, connected }: { me: Me; connected: boolean }) {
  const mcp = useMCPAddress();
  // The token card is fetched only once its route is opened.
  const [opened, setOpened] = useState(false);
  // Someone who may neither create nor revoke tokens still has the route while they hold a token: their sessions say.
  const byPermission = agentTokensShown(me);
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions, enabled: !byPermission || opened });
  // A token created or revoked in the card reloads the sessions: the step's state follows at once.
  useSetupFollows(sessions.dataUpdatedAt);
  const tokens = byPermission || agentTokensShown(me, sessions.data?.items);
  return <div className="setup-agent">
    {connected && <p className="setup-done" role="status"><span className="setup-done-mark">{m.connected}</span>
      <span>{m.manageBefore}<a href="/settings/account">{m.apps}</a>{tokens && <>{m.manageBetween}<a href={tokensAddress}>{m.tokens}</a></>}{m.manageAfter}</span></p>}
    {mcp?.cloud && <section className="setup-agent-way" aria-label={m.claudeTitle}>
      <h2>{m.claudeTitle}</h2>
      <p>{m.claudeText}</p>
      <p><a className="button" href={claudeDirectoryURL} target="_blank" rel="noopener noreferrer">{m.addToClaude} <ExternalLinkIcon /><span className="sr-only">{m.newTab}</span></a></p>
    </section>}
    {mcp && <section className="setup-agent-way" aria-label={m.anyAppTitle}>
      <h2>{m.anyAppTitle}</h2>
      <p>{m.anyAppText}</p>
      <p className="setup-agent-address"><span className="field-label">{m.address}</span> <code>{mcp.address}</code>{' '}
        <CopyButton value={mcp.address} label={m.copyAddress}>{m.copy}</CopyButton></p>
    </section>}
    {tokens && <details className="setup-agent-token" onToggle={(event) => { if (event.currentTarget.open) setOpened(true); }}>
      <summary>{m.tokenRoute}</summary>
      <p className="muted-copy">{m.tokenText}</p>
      {opened && <AIAgentsSections />}
    </details>}
    <p className="muted-copy"><a href={messages.mcpURL} target="_blank" rel="noopener noreferrer">{m.guide}</a></p>
  </div>;
}
