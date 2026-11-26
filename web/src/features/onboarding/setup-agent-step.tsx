import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { ExternalLinkIcon } from '../../ui/icons';
import { AIAgentsSections, agentTokensShown } from '../settings/ai-agents-sections';
import { pluginMCPURL } from '../settings/connect-snippets.generated';
import { CopyButton } from '../settings/copy-button';
import { agentStepMessages as m } from './setup-flow-messages';
import './setup-agent-step.css';

// Where Claude lists Colliedesk Cloud as a connector: Connect there signs in with OAuth, as a custom connector does.
export const claudeDirectoryURL = 'https://claude.ai/directory/connectors/colliedesk';

// AgentStep connects an AI app without a key first: on Colliedesk Cloud, Add to Claude opens Claude's directory;
// any app (and a server of its own) adds this server's MCP address and signs in with OAuth. An agent token, the
// Settings card with its steps per app, is the second choice. Connected, it says where to disconnect.
export function AgentStep({ me, connected }: { me: Me; connected: boolean }) {
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  // Where an AI app signs in: APP_ORIGIN's /mcp, one address for every workspace on Cloud.
  const mcpAddress = `${config.data?.app_origin || window.location.origin}/mcp`;
  // Claude's directory lists the Cloud connector only; a server of its own is added by its address.
  const cloud = mcpAddress === pluginMCPURL;
  return <div className="setup-flow-body setup-agent">
    {connected && <p className="setup-done" role="status"><span className="setup-done-mark">{m.connected}</span>
      <span>{m.manageBefore}<a href="/settings/account">{m.apps}</a>{m.manageBetween}<a href="/settings/ai">{m.tokens}</a>{m.manageAfter}</span></p>}
    {cloud && <section className="setup-agent-way" aria-label={m.claudeTitle}>
      <h2>{m.claudeTitle}</h2>
      <p>{m.claudeText}</p>
      <p><a className="button primary-action" href={claudeDirectoryURL} target="_blank" rel="noopener noreferrer">{m.addToClaude} <ExternalLinkIcon /><span className="sr-only">{m.newTab}</span></a></p>
    </section>}
    <section className="setup-agent-way" aria-label={m.anyAppTitle}>
      <h2>{m.anyAppTitle}</h2>
      <p>{m.anyAppText}</p>
      <p className="setup-agent-address"><span className="field-label">{m.address}</span> <code>{mcpAddress}</code>{' '}
        <CopyButton value={mcpAddress} label={m.copyAddress}>{m.copy}</CopyButton></p>
    </section>
    {agentTokensShown(me) && <details className="setup-agent-token">
      <summary>{m.tokenRoute}</summary>
      <p className="muted-copy">{m.tokenText}</p>
      <AIAgentsSections />
    </details>}
    <p className="muted-copy"><a href={messages.mcpURL} target="_blank" rel="noopener noreferrer">{m.guide}</a></p>
  </div>;
}
