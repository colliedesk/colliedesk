import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { InvalidRequestError, PlanLimitError, UnauthorizedError } from '../../api/client';
import type { AgentToken, Role, RoleRef, TokenPermission } from '../../api/schemas';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { CopyButton } from './copy-button';
import { connectHosts, connectSnippets, exampleOrigin, mcpServerName } from './connect-snippets.generated';
import type { ConnectHostID } from './connect-snippets.generated';
import { outsidePermissions, permissionSummary } from './token-permissions';
import { PermissionMatrix } from './permission-matrix';
import { roleMessages } from './roles-messages';
import './agent-tokens.css';

// The roles a token may follow: any of the workspace whose permissions the person all holds (every role
// for an admin), or only their own roles while the workspace's roles cannot be read.
export function followableRoles(own: RoleRef[], workspace: Role[] | undefined, held: readonly string[]): RoleRef[] {
  if (!workspace) return own;
  const holds = new Set(held);
  return workspace.filter((role) => role.deleted_at === null && role.permissions.every((name) => holds.has(name)))
    .map(({ id, name, system }) => ({ id, name, system }));
}

// What a token is created with: a role it follows, or a fixed list.
export type TokenChoice = { role_id: string } | { permissions: TokenPermission[] };

interface AgentTokensSectionProps {
  // The roles a token may follow (followableRoles), and what each holds when the person may read roles;
  // held is what they may do now.
  roles: RoleRef[];
  rolePermissions?: ReadonlyMap<string, readonly string[]>;
  held: readonly string[];
  onCreate: (label: string, choice: TokenChoice) => Promise<AgentToken>;
  // origin is the address this app is open at; the steps use it in place of the guides' example.
  origin?: string;
}

// cursorInstallLink is Cursor's install deeplink (cursor.com/docs/context/mcp/install-links): the server
// entry alone, as base64 JSON. It holds the token, so it exists only while the token is shown.
function cursorInstallLink(origin: string, token: string): string {
  const config = btoa(JSON.stringify({ url: `${origin}/mcp`, headers: { Authorization: `Bearer ${token}` } }));
  return `cursor://anysphere.cursor-deeplink/mcp/install?${new URLSearchParams({ name: mcpServerName, config }).toString()}`;
}

// refusal says why no token came back. A refusal the server explained created nothing; any other failure
// may have come after the server created one, so it points to Sessions instead of saying none exists.
function refusal(error: unknown): string {
  if (error instanceof InvalidRequestError || error instanceof PlanLimitError) return error.message;
  if (error instanceof UnauthorizedError) return text.signedOut;
  return text.createUnconfirmed;
}

// AgentTokensSection creates an agent token and shows it once, then how each AI host uses it. The token
// lives in this component's state only: no query cache, no storage, gone once hidden or the page is left.
export function AgentTokensSection({ roles, rolePermissions, held, onCreate, origin = window.location.origin }: AgentTokensSectionProps) {
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
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<{ message: string; field: boolean } | null>(null);
  const [created, setCreated] = useState<{ token: string; label: string } | null>(null);
  const [hidden, setHidden] = useState(false);
  const [host, setHost] = useState<ConnectHostID>('claude-code');
  const nameInput = useRef<HTMLInputElement>(null);
  const tokenInput = useRef<HTMLTextAreaElement>(null);
  const another = useRef<HTMLButtonElement>(null);
  // Each new state of step 1 takes the focus to what comes next there.
  const [focus, setFocus] = useState<'name' | 'token' | 'another' | null>(null);
  useEffect(() => {
    if (focus === 'name') nameInput.current?.focus();
    if (focus === 'token') tokenInput.current?.select();
    if (focus === 'another') another.current?.focus();
  }, [focus]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
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
      setFocus('token');
    } catch (caught) {
      setError({ message: refusal(caught), field: false });
    } finally {
      setPending(false);
    }
  };
  const hide = () => {
    setCreated(null);
    setHidden(true);
    setFocus('another');
  };
  const startAgain = () => {
    setHidden(false);
    setLabel('');
    setFocus('name');
  };
  const token = created?.token;

  return (
    <section className="settings-card agent-tokens" aria-labelledby="agents-heading">
      <div className="section-heading"><h2 id="agents-heading">{text.heading}</h2><p>{text.description}</p></div>
      <div className="settings-body">
        <div className="agent-step">
          <h3 className="step-title"><span className="step-no" aria-hidden="true">1</span>{text.createStep}</h3>
          {!created && !hidden && <form className="token-form" noValidate onSubmit={(event) => void submit(event)}>
            <label className="field"><span>{text.name}</span>
              <input ref={nameInput} value={label} maxLength={80} placeholder={text.namePlaceholder} autoComplete="off"
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
            <p className="muted-copy">{text.lifetime}</p>
            {error && <p role="alert" id="agent-token-error" className="error-copy">{error.message}</p>}
            <span className="button-row"><button className="primary-action" type="submit" disabled={pending}>{pending ? text.creating : text.create}</button></span>
          </form>}
          {created && <div className="token-created">
            <div className="copy-field">
              <label className="invite-link"><span className="field-label">{text.yourToken(created.label)}</span>
                <textarea ref={tokenInput} readOnly rows={2} spellCheck={false} className="token-value" value={created.token} /></label>
              <span className="button-row"><CopyButton value={created.token} className="primary-action">{text.copyToken}</CopyButton></span>
            </div>
            <p className="warning-note">{text.shownOnce}</p>
            <span className="button-row"><button type="button" onClick={hide}>{text.hideToken}</button></span>
          </div>}
          {hidden && <p className="muted-copy token-hidden-note">{text.hiddenBefore}<strong>{text.hiddenRevoke}</strong>{text.hiddenAfter}
            <button ref={another} type="button" className="link-button" onClick={startAgain}>{text.createAnother}</button></p>}
        </div>

        <div className="agent-step">
          <h3 className="step-title"><span className="step-no" aria-hidden="true">2</span>{text.connectStep}</h3>
          <div className="status-tabs scroll-x-fade host-tabs" role="radiogroup" aria-label={text.host}>
            {connectHosts.map((item) => <label key={item.id}><input type="radio" name="agent-host" value={item.id} checked={host === item.id}
              onChange={() => setHost(item.id)} />{item.name}</label>)}
          </div>
          <HostSteps host={host} origin={origin} token={token} hidden={hidden} />
          <p className="muted-copy check-it">{text.checkIt}</p>
        </div>
      </div>
    </section>
  );
}

// hidden is a token just created and hidden: the steps say so instead of asking for a first one.
function HostSteps({ host, origin, token, hidden }: { host: ConnectHostID; origin: string; token?: string; hidden: boolean }) {
  const item = connectHosts.find((candidate) => candidate.id === host)!;
  const snippet = (name: keyof typeof connectSnippets) => connectSnippets[name].replaceAll(exampleOrigin, origin);
  const [before, command] = text.disconnects[host];
  let steps: ReactNode;
  if (host === 'claude-code') {
    steps = <>
      <ol className="host-steps">
        <li><Snippet label={text.runInTerminal} code={snippet('claudeCodePlugin')} /><p className="muted-copy">{text.pluginInstalls}</p></li>
        <li><Snippet label={text.insideClaudeCode} code={snippet('claudeCodeToken')} /><p className="muted-copy">{text.pluginToken}</p></li>
      </ol>
      <details className="host-alt"><summary>{text.withoutPlugin}</summary>
        <ol className="host-steps">
          <li><Snippet label={text.shellProfile} code={snippet('tokenEnv')} /></li>
          <li><Snippet label={text.thenRun} code={snippet('claudeCodeCLI')} /></li>
        </ol>
      </details>
    </>;
  } else if (host === 'codex') {
    steps = <ol className="host-steps">
      <li><Snippet label={text.shellProfile} code={snippet('tokenEnv')} /><p className="muted-copy">{text.codexReadsEnv}</p></li>
      <li><Snippet label={text.thenRun} code={snippet('codex')} /></li>
    </ol>;
  } else if (host === 'cursor') {
    steps = <>
      <div className="deeplink">
        {token ? <a className="button primary-action" href={cursorInstallLink(origin, token)}>{text.addToCursor}</a>
          : <button type="button" className="primary-action" aria-disabled="true">{text.addToCursor}</button>}
        <p className="muted-copy">{token ? text.cursorWithToken : hidden ? text.cursorHidden : text.cursorNoToken}</p>
      </div>
      <details className="host-alt"><summary>{text.byHand}</summary>
        <ol className="host-steps">
          <li><Snippet label={text.shellProfile} code={snippet('tokenEnv')} /></li>
          <li><Snippet label={text.cursorFile} code={snippet('cursor')} /><p className="muted-copy">{text.cursorEnv}</p></li>
        </ol>
      </details>
    </>;
  } else if (host === 'vscode') {
    steps = <ol className="host-steps">
      <li><p>{text.vscodeOpen}<strong>{text.vscodeCommand}</strong>.</p></li>
      <li><Snippet label={text.vscodeFile} code={snippet('vscode')} /><p className="muted-copy">{text.vscodePrompt}</p></li>
    </ol>;
  } else {
    steps = <ol className="host-steps">
      <li><p>{text.desktopDownloadStep}</p>
        <span className="button-row"><a className="button" href={connectSnippets.desktopBundle}>{text.desktopDownload}</a>
          <a className="link-button" href={text.releasePage} target="_blank" rel="noopener noreferrer">{text.desktopChecksums}</a></span></li>
      <li><p>{text.desktopOpen}</p>
        <div className="copy-field"><label className="invite-link"><span className="field-label">{text.desktopAddress}</span><input readOnly value={origin} /></label>
          <span className="button-row"><CopyButton value={origin}>{text.copyAddress}</CopyButton></span></div>
        <p className="muted-copy">{text.desktopToken}</p></li>
    </ol>;
  }
  return (
    <div className="host-panel" role="group" aria-label={item.name}>
      {steps}
      <p className="muted-copy host-foot"><a href={item.guide} target="_blank" rel="noopener noreferrer">{text.fullGuide}</a>
        {' · '}{text.disconnect}{before}{command && <code>{command}</code>}{text.revokeAfter}</p>
    </div>
  );
}

function Snippet({ label, code }: { label: string; code: string }) {
  return (
    <div className="snippet">
      <div className="snippet-head"><span className="field-label">{label}</span>
        <span className="button-row"><CopyButton value={code} label={text.copySnippet(label)}>{text.copy}</CopyButton></span></div>
      <pre tabIndex={0} role="group" aria-label={label}><code>{code}</code></pre>
    </div>
  );
}
