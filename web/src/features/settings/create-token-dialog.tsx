import { useEffect, useRef, useState } from 'react';
import { InvalidRequestError, PlanLimitError, UnauthorizedError } from '../../api/client';
import type { AgentToken, RoleRef, TokenPermission } from '../../api/schemas';
import { Dialog } from '../../ui/dialog';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { ConnectAgent } from './connect-agent';
import type { ConnectHostID } from './connect-snippets.generated';
import { CopyButton } from './copy-button';
import { PermissionMatrix } from './permission-matrix';
import { outsidePermissions, permissionSummary } from './token-permissions';

// What a token is created with: a role it follows, or a fixed list.
export type TokenChoice = { role_id: string } | { permissions: TokenPermission[] };

export interface CreateTokenDialogProps {
  // The roles a token may follow (followableRoles), and what each holds when the person may read roles;
  // held is what they may do now.
  roles: RoleRef[];
  rolePermissions?: ReadonlyMap<string, readonly string[]>;
  held: readonly string[];
  onCreate: (label: string, choice: TokenChoice) => Promise<AgentToken>;
  // The host picked under the token list; the dialog shows its steps with the new token.
  host: ConnectHostID;
  onHost: (host: ConnectHostID) => void;
  origin: string;
  mcpAddress: string;
  // shown says whether the dialog closes after a token was shown, so the page can say it was hidden.
  onClose: (shown: boolean) => void;
}

// refusal says why no token came back. A refusal the server explained created nothing; any other failure
// may have come after the server created one, so it points to the list instead of saying none exists.
function refusal(error: unknown): string {
  if (error instanceof InvalidRequestError || error instanceof PlanLimitError) return error.message;
  if (error instanceof UnauthorizedError) return text.signedOut;
  return text.createUnconfirmed;
}

// CreateTokenDialog creates an agent token, then shows it once with the steps to connect an agent. The token
// lives in this dialog's state only: no query cache, no storage, gone once the dialog closes.
export function CreateTokenDialog({ roles, rolePermissions, held, onCreate, host, onHost, origin, mcpAddress, onClose }: CreateTokenDialogProps) {
  const [label, setLabel] = useState('');
  const permissionsOf = (id: string | undefined): readonly string[] => (id === undefined ? [] : rolePermissions?.get(id) ?? []);
  // The role with the fewest permissions comes first, and is the one a new token follows by default.
  const ordered = [...roles].sort((a, b) => (rolePermissions ? permissionsOf(a.id).length - permissionsOf(b.id).length : 0));
  const [mode, setMode] = useState<'role' | 'pick'>('role');
  // Only a role the person picked is kept; until then, and when it may no longer be followed, the default
  // is the role with the fewest permissions among those it may follow now.
  const [roleId, setRoleId] = useState<string>();
  const role = roleId !== undefined && ordered.some((item) => item.id === roleId) ? roleId : ordered[0]?.id;
  // A fixed list starts from the permissions of the role chosen above.
  const [picked, setPicked] = useState<TokenPermission[]>([]);
  const [showRole, setShowRole] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<{ message: string; field: boolean } | null>(null);
  const [created, setCreated] = useState<{ token: string; label: string } | null>(null);
  const nameInput = useRef<HTMLInputElement>(null);
  const tokenInput = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (created) tokenInput.current?.select(); }, [created]);

  const pick = () => {
    setMode('pick');
    setPicked(permissionsOf(role).filter((name) => held.includes(name)) as TokenPermission[]);
    if (error && !error.field) setError(null);
  };
  const toggle = (permission: TokenPermission) =>
    setPicked((current) => (current.includes(permission) ? current.filter((item) => item !== permission) : [...current, permission]));
  const setMany = (permissions: TokenPermission[], on: boolean) =>
    setPicked((current) => (on ? [...new Set([...current, ...permissions])] : current.filter((item) => !permissions.includes(item))));
  const allowed = new Set(held);
  const rolePermissionsNow = permissionsOf(role);
  const wide = rolePermissionsNow.some((name) => outsidePermissions.has(name as TokenPermission) || name.startsWith('team:'));
  // Without Send to customers, an agent that may leave AI drafts answers customers through a person.
  const chosen: readonly string[] = mode === 'pick' ? picked : rolePermissionsNow;
  const draftsOnly = chosen.includes('ai_assist:create') && !chosen.includes('messages:send');

  const submit = async () => {
    if (pending) return;
    const name = label.trim();
    if (!name) {
      setError({ message: text.nameMissing, field: true });
      nameInput.current?.focus();
      return;
    }
    if (mode === 'pick' && picked.length === 0) {
      setError({ message: text.permissionsMissing, field: false });
      return;
    }
    if (mode === 'role' && !role) {
      setError({ message: text.noRole, field: false });
      return;
    }
    setPending(true);
    setError(null);
    try {
      const answer = await onCreate(name, mode === 'pick' ? { permissions: picked } : { role_id: role! });
      setCreated({ token: answer.token, label: name });
    } catch (caught) {
      setError({ message: refusal(caught), field: false });
    } finally {
      setPending(false);
    }
  };

  // One dialog from the form to the token it made, so the focus stays in it.
  const close = () => onClose(Boolean(created));
  if (created) {
    return <Dialog title={text.createTitle} closeLabel={text.close} className="token-dialog" onClose={close} onSubmit={close}
      footer={<button type="submit">{text.hideToken}</button>}>
      <div className="token-created">
        <label className="invite-link"><span className="field-label">{text.yourToken(created.label)}</span>
          <textarea ref={tokenInput} readOnly rows={2} spellCheck={false} className="token-value" value={created.token} /></label>
        <span className="button-row"><CopyButton value={created.token} className="primary-action">{text.copyToken}</CopyButton></span>
        <p className="warning-note">{text.shownOnce}</p>
      </div>
      <div className="token-dialog-connect"><ConnectAgent host={host} onHost={onHost} origin={origin} mcpAddress={mcpAddress} token={created.token} hidden={false} /></div>
    </Dialog>;
  }
  return <Dialog title={text.createTitle} closeLabel={text.close} className="token-dialog" onClose={close} onSubmit={() => void submit()}
    footer={<><button type="button" onClick={close}>{text.cancel}</button>
      <button className="primary-action" type="submit" disabled={pending}>{pending ? text.creating : text.create}</button></>}>
    <label className="field"><span>{text.name}</span>
      <input ref={nameInput} autoFocus value={label} maxLength={80} placeholder={text.namePlaceholder} autoComplete="off"
        aria-invalid={error?.field ? true : undefined} aria-describedby={error?.field ? 'agent-token-error' : undefined}
        onChange={(event) => { setLabel(event.target.value); if (error?.field) setError(null); }} /></label>
    <fieldset className="access-field"><legend className="field-label">{text.access}</legend>
      <div className="access-option">
        <label><input type="radio" name="agent-token-access" checked={mode === 'role'} onChange={() => setMode('role')} />{text.followRole}</label>
        <p className="muted-copy">{text.followRoleHelp}</p>
        {mode === 'role' && <>
          {ordered.length > 1
            ? <label className="field"><span className="sr-only">{text.role}</span>
              <select value={role ?? ''} onChange={(event) => setRoleId(event.target.value)}>
                {ordered.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select></label>
            : ordered[0] && <p className="muted-copy">{text.role}: <strong>{ordered[0].name}</strong></p>}
          {rolePermissions && <p className="muted-copy">{permissionSummary(rolePermissionsNow)}{' '}
            <button type="button" className="link-button" aria-expanded={showRole} aria-controls="agent-token-role-matrix"
              onClick={() => setShowRole((shown) => !shown)}>{showRole ? text.hidePermissions : text.showThem}</button></p>}
          {wide && <p className="warning-note">{text.wideRole}</p>}
          {showRole && <div id="agent-token-role-matrix"><PermissionMatrix idPrefix="token-role" picked={rolePermissionsNow as TokenPermission[]} allowed={allowed} readOnly /></div>}
        </>}
      </div>
      <div className="access-option">
        <label><input type="radio" name="agent-token-access" checked={mode === 'pick'} aria-controls="token-pick-matrix" onChange={pick} />{text.pickPermissions}</label>
        {mode === 'pick' && <>
          <p className="muted-copy">{permissionSummary(picked)}</p>
          <PermissionMatrix idPrefix="token-pick" picked={picked} allowed={allowed} onToggle={toggle} onSetMany={setMany} />
        </>}
      </div>
    </fieldset>
    {draftsOnly && <p className="drafts-note"><span className="token-dot token-dot-hollow" aria-hidden="true" /><span><strong>{text.draftsOnly}</strong>{text.draftsOnlyRest}</span></p>}
    <p className="muted-copy">{text.lifetime}</p>
    {error && <p role="alert" id="agent-token-error" className="error-copy">{error.message}</p>}
  </Dialog>;
}
