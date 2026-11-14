import { useState } from 'react';
import type { Role, RoleRef, Session } from '../../api/schemas';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { AgentTokenList } from './agent-token-list';
import { ConnectAgent } from './connect-agent';
import type { ConnectHostID } from './connect-snippets.generated';
import { CreateTokenDialog } from './create-token-dialog';
import type { CreateTokenDialogProps } from './create-token-dialog';
import './agent-tokens.css';

export type { TokenChoice } from './create-token-dialog';

// The roles a token may follow: any of the workspace whose permissions the person all holds (every role
// for an admin), or only their own roles while the workspace's roles cannot be read.
export function followableRoles(own: RoleRef[], workspace: Role[] | undefined, held: readonly string[]): RoleRef[] {
  if (!workspace) return own;
  const holds = new Set(held);
  return workspace.filter((role) => role.deleted_at === null && role.permissions.every((name) => holds.has(name)))
    .map(({ id, name, system }) => ({ id, name, system }));
}

type AgentTokensSectionProps = Pick<CreateTokenDialogProps, 'roles' | 'rolePermissions' | 'held'> & {
  // How to create a token; missing for someone who may only see and revoke tokens (no Create token, no setup steps).
  onCreate?: CreateTokenDialogProps['onCreate'];
  // origin is the address this app is open at; the steps use it in place of the guides' example.
  origin?: string;
  // mcpAddress is where an app signs in with OAuth (APP_ORIGIN's /mcp). Claude Code uses it: when it is the
  // address the Colliedesk plugin connects to (Cloud), the plugin; otherwise this server added by hand.
  mcpAddress?: string;
  // The agent tokens the person may see (theirs, and with team:revoke their teammates'), and how to revoke one.
  tokens?: Session[];
  timezone?: string;
  onRevoke?: (id: string) => void;
};

// AgentTokensSection is Settings → External AI agents: the agent tokens, each with what it may do, when it was
// last used and a way to revoke it; Create token opens the form in a dialog that shows the new token once;
// Connect your agent stays below the list.
export function AgentTokensSection({ roles, rolePermissions, held, onCreate, origin = window.location.origin, mcpAddress = `${origin}/mcp`,
  tokens = [], timezone = 'UTC', onRevoke = () => {} }: AgentTokensSectionProps) {
  const [creating, setCreating] = useState(false);
  // A token was shown and hidden since: Add to Cursor says so instead of asking for a first token.
  const [hidden, setHidden] = useState(false);
  const [host, setHost] = useState<ConnectHostID>('claude-code');
  return (
    <section className="settings-card agent-tokens" aria-labelledby="agents-heading">
      <div className="section-heading"><h2 id="agents-heading">{text.heading}</h2></div>
      <div className="settings-body">
        <div className="agent-step">
          <div className="tokens-head">
            <div><h3 id="agent-tokens-heading" className="step-title">{text.tokensHeading}</h3><p className="muted-copy">{text.description}</p></div>
            {onCreate && <button type="button" className="primary-action" onClick={() => setCreating(true)}>{text.create}</button>}
          </div>
          <AgentTokenList tokens={tokens} timezone={timezone} onRevoke={onRevoke} labelledBy="agent-tokens-heading" />
        </div>
        {onCreate && <div className="agent-step">
          <ConnectAgent host={host} onHost={setHost} origin={origin} mcpAddress={mcpAddress} hidden={hidden} />
        </div>}
      </div>
      {creating && onCreate && <CreateTokenDialog roles={roles} rolePermissions={rolePermissions} held={held} onCreate={onCreate} host={host} onHost={setHost}
        origin={origin} mcpAddress={mcpAddress} onClose={(shown) => { setCreating(false); if (shown) setHidden(true); }} />}
    </section>
  );
}
