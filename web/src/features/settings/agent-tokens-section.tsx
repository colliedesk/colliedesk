import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { InvalidRequestError, PlanLimitError, UnauthorizedError } from '../../api/client';
import type { AgentToken, TokenScope } from '../../api/schemas';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { connectHosts, connectSnippets, exampleOrigin, mcpServerName } from './connect-snippets.generated';
import type { ConnectHostID } from './connect-snippets.generated';
import './agent-tokens.css';

const scopes: TokenScope[] = ['read', 'draft', 'send', 'admin'];

interface AgentTokensSectionProps {
  // canCreateAdmin is the signed-in person's admin role: the server refuses an admin token to anyone else.
  canCreateAdmin: boolean;
  onCreate: (label: string, scope: TokenScope) => Promise<AgentToken>;
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
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<{ message: string; field: boolean } | null>(null);
  const [created, setCreated] = useState<{ token: string; label: string } | null>(null);
  const [hidden, setHidden] = useState(false);
  const [host, setHost] = useState<ConnectHostID>('claude-code');
  const nameInput = useRef<HTMLInputElement>(null);
  const tokenInput = useRef<HTMLInputElement>(null);
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
    setPending(true);
    setError(null);
    try {
      const answer = await onCreate(name, scope);
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
          <p className="step-title"><span className="step-no" aria-hidden="true">1</span>{text.createStep}</p>
          {!created && !hidden && <form className="token-form" noValidate onSubmit={(event) => void submit(event)}>
            <label className="field"><span>{text.name}</span>
              <input ref={nameInput} value={label} maxLength={80} placeholder={text.namePlaceholder} autoComplete="off"
                aria-invalid={error?.field ? true : undefined} aria-describedby={error?.field ? 'agent-token-error' : undefined}
                onChange={(event) => { setLabel(event.target.value); if (error?.field) setError(null); }} /></label>
            <fieldset className="tier-field"><legend className="field-label">{text.tier}</legend>
              <div className="segmented tier-picker">
                {scopes.map((value) => <label key={value}><input type="radio" name="agent-token-tier" value={value} checked={scope === value}
                  disabled={value === 'admin' && !canCreateAdmin} onChange={() => setScope(value)} />{text.tiers[value]}</label>)}
              </div>
              <p className="muted-copy">{text.tierHelp[scope]}</p>
              {!canCreateAdmin && <p className="muted-copy">{text.adminOnly}</p>}
            </fieldset>
            <p className="muted-copy">{text.lifetime}</p>
            {error && <p role="alert" id="agent-token-error" className="error-copy">{error.message}</p>}
            <span className="button-row"><button className="primary-action" type="submit" disabled={pending}>{pending ? text.creating : text.create}</button></span>
          </form>}
          {created && <div className="token-created">
            <div className="copy-field">
              <label className="invite-link"><span className="field-label">{text.yourToken(created.label)}</span>
                <input ref={tokenInput} readOnly className="token-value" value={created.token} /></label>
              <span className="button-row"><CopyButton value={created.token} className="primary-action">{text.copyToken}</CopyButton></span>
            </div>
            <p className="warning-note">{text.shownOnce}</p>
            <span className="button-row"><button type="button" onClick={hide}>{text.hideToken}</button></span>
          </div>}
          {hidden && <p className="muted-copy token-hidden-note">{text.hiddenBefore}<strong>{text.hiddenRevoke}</strong>{text.hiddenAfter}
            <button ref={another} type="button" className="link-button" onClick={startAgain}>{text.createAnother}</button></p>}
        </div>

        <div className="agent-step">
          <p className="step-title"><span className="step-no" aria-hidden="true">2</span>{text.connectStep}</p>
          <div className="status-tabs scroll-x-fade host-tabs" role="radiogroup" aria-label={text.host}>
            {connectHosts.map((item) => <label key={item.id}><input type="radio" name="agent-host" value={item.id} checked={host === item.id}
              onChange={() => setHost(item.id)} />{item.name}</label>)}
          </div>
          <HostSteps host={host} origin={origin} token={token} />
          <p className="muted-copy check-it">{text.checkIt}</p>
        </div>
      </div>
    </section>
  );
}

function HostSteps({ host, origin, token }: { host: ConnectHostID; origin: string; token?: string }) {
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
          : <a className="button primary-action" role="link" aria-disabled="true">{text.addToCursor}</a>}
        <p className="muted-copy">{token ? text.cursorWithToken : text.cursorNoToken}</p>
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
      <pre tabIndex={0} aria-label={label}><code>{code}</code></pre>
    </div>
  );
}

// CopyButton puts value on the clipboard and says so next to itself for two seconds.
function CopyButton({ value, label, className, children }: { value: string; label?: string; className?: string; children: ReactNode }) {
  const [state, setState] = useState<'copied' | 'failed' | null>(null);
  useEffect(() => {
    if (!state) return undefined;
    const timer = window.setTimeout(() => setState(null), 2000);
    return () => window.clearTimeout(timer);
  }, [state]);
  const copy = () => {
    const write = navigator.clipboard?.writeText(value) ?? Promise.reject(new Error('no clipboard'));
    write.then(() => setState('copied'), () => setState('failed'));
  };
  return <>
    <button type="button" className={className} aria-label={label} onClick={copy}>{children}</button>
    <span role="status" className={state === 'failed' ? 'error-copy' : 'copied'}>{state === 'copied' ? text.copied : state === 'failed' ? text.copyFailed : ''}</span>
  </>;
}
