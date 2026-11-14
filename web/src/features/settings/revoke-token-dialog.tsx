import type { Session } from '../../api/schemas';
import { Dialog } from '../../ui/dialog';
import { agentTokenMessages as text } from './agent-tokens-messages';
import './agent-tokens.css';

// RevokeTokenDialog asks before an agent token is revoked, on External AI agents and in Sessions alike: a
// revoked token cannot be brought back. It names the token, says the agent is refused from its next call,
// and that what the agent wrote keeps its "by <person> via <token>" label.
export function RevokeTokenDialog({ session, onClose, onRevoke }: { session: Session; onClose: () => void; onRevoke: (id: string) => void }) {
  return (
    <Dialog alert title={text.revokeTitle(session.label)} closeLabel={text.cancel}
      className="revoke-token-dialog" onClose={onClose} onSubmit={() => { onClose(); onRevoke(session.id); }}
      footer={<><button type="button" autoFocus onClick={onClose}>{text.cancel}</button><button type="submit" className="token-danger-solid">{text.revoke}</button></>}>
      {/* The consequence right before an action that cannot be undone stays in sight. */}
      <p data-quiet="consequence">{text.revokeBody(session.agent_name, session.label)}</p>
    </Dialog>
  );
}
