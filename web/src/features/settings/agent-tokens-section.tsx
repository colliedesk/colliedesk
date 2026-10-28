import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { InvalidRequestError, PlanLimitError, UnauthorizedError } from '../../api/client';
import type { AgentToken, TokenPermission, TokenPreset, TokenScope } from '../../api/schemas';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { CopyButton } from './copy-button';
import { connectHosts, connectSnippets, exampleOrigin, mcpServerName } from './connect-snippets.generated';
import type { ConnectHostID } from './connect-snippets.generated';
import { actionLabel, presetPermissions, tokenResources } from './token-permissions';
import './agent-tokens.css';

const scopes: TokenScope[] = ['read', 'draft', 'send', 'admin', 'custom'];

// What a token is created with: a tier, or its own permissions.
export type TokenChoice = { scope: TokenPreset } | { permissions: TokenPermission[] };

interface AgentTokensSectionProps {
  // canCreateAdmin is the signed-in person's admin role: the server refuses an admin token to anyone else.
  canCreateAdmin: boolean;
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
export function AgentTokensSection({ canCreateAdmin, onCreate, origin = window.location.origin }: AgentTokensSectionProps) {
  const [label, setLabel] = useState('');
  const [scope, setScope] = useState<TokenScope>('draft');
  // The permissions the form shows ticked: the tier's while a tier is chosen, the person's own once custom.
  const [picked, setPicked] = useState<TokenPermission[]>(() => presetPermissions('draft'));
  const [showPermissions, setShowPermissions] = useState(false);
  const choose = (value: TokenScope) => {
    setScope(value);
    if (value === 'custom') setShowPermissions(true);
    else setPicked(presetPermissions(value));
    if (error && !error.field) setError(null);
  };
  // Changing a permission makes the token custom, starting from what the tier held.
  const toggle = (permission: TokenPermission) => {
    setScope('custom');
    setPicked((current) => (current.includes(permission) ? current.filter((item) => item !== permission) : [...current, permission]));
  };
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
    if (scope === 'custom' && picked.length === 0) {
      setError({ message: text.permissionsMissing, field: false });
      return;
    }
    setPending(true);
    setError(null);
    try {
      const answer = await onCreate(name, scope === 'custom' ? { permissions: picked } : { scope });
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
            <fieldset className="tier-field"><legend className="field-label">{text.tier}</legend>
              <div className="segmented tier-picker">
                {scopes.map((value) => <label key={value}><input type="radio" name="agent-token-tier" value={value} checked={scope === value}
                  disabled={value === 'admin' && !canCreateAdmin} onChange={() => choose(value)} />{text.tiers[value]}</label>)}
              </div>
              <p className="muted-copy">{text.tierHelp[scope]}</p>
              {!canCreateAdmin && <p className="muted-copy">{text.adminOnly}</p>}
              <button type="button" className="link-button permissions-toggle" aria-expanded={showPermissions} aria-controls="agent-token-permissions"
                onClick={() => setShowPermissions((shown) => !shown)}>{showPermissions ? text.hidePermissions : text.showPermissions(picked.length)}</button>
              {showPermissions && <PermissionMatrix picked={picked} canCreateAdmin={canCreateAdmin} onToggle={toggle} />}
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

// PermissionMatrix lists every resource with a chip for each action the token may take on it. Ticking
// or unticking one makes the token custom. An admin-only one is locked for a person who is not an admin.
function PermissionMatrix({ picked, canCreateAdmin, onToggle }: { picked: TokenPermission[]; canCreateAdmin: boolean; onToggle: (permission: TokenPermission) => void }) {
  return (
    <div id="agent-token-permissions" className="permission-matrix">
      <p className="permission-legend">
        <span><span className="permission-mark outside" aria-hidden="true" />{text.outsideLegend}</span>
        <span><span className="permission-mark asks" aria-hidden="true" />{text.asksLegend}</span>
        {!canCreateAdmin && <span><span className="permission-mark locked" aria-hidden="true" />{text.lockedLegend}</span>}</p>
      <ul>
        {tokenResources.map((resource) => (
          <li key={resource.resource}>
            <span className="permission-resource" id={`perm-${resource.resource}`}>{resource.label}</span>
            <span className="permission-chips" role="group" aria-labelledby={`perm-${resource.resource}`}>
              {resource.permissions.map((item) => {
                const locked = item.preset === 'admin' && !canCreateAdmin;
                const notes = [item.outside && text.outside, item.asks && text.asks, locked && text.adminOnlyPermission].filter(Boolean).join(' · ');
                return (
                  <label key={item.name} className="permission-chip" title={notes || undefined}>
                    <input type="checkbox" checked={picked.includes(item.name)} disabled={locked} onChange={() => onToggle(item.name)}
                      aria-description={notes || undefined} />
                    {actionLabel(item.name)}
                    {item.outside && <span className="permission-mark outside" aria-hidden="true" />}
                    {item.asks && <span className="permission-mark asks" aria-hidden="true" />}
                  </label>
                );
              })}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
