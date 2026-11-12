import { useId } from 'react';
import type { ReactNode } from 'react';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { CopyButton } from './copy-button';
import { connectHosts, connectSnippets, exampleOrigin, mcpServerName, pluginMCPURL } from './connect-snippets.generated';
import type { ConnectHostID } from './connect-snippets.generated';

// cursorInstallLink is Cursor's install deeplink (cursor.com/docs/context/mcp/install-links): the server
// entry alone, as base64 JSON. It holds the token, so it exists only while the token is shown.
function cursorInstallLink(origin: string, token: string): string {
  const config = btoa(JSON.stringify({ url: `${origin}/mcp`, headers: { Authorization: `Bearer ${token}` } }));
  return `cursor://anysphere.cursor-deeplink/mcp/install?${new URLSearchParams({ name: mcpServerName, config }).toString()}`;
}

interface ConnectAgentProps {
  host: ConnectHostID;
  onHost: (host: ConnectHostID) => void;
  origin: string;
  mcpAddress: string;
  // token is the one just created, while it is shown; hidden says one was created and hidden since.
  token?: string;
  hidden: boolean;
}

// ConnectAgent is "Connect your agent": a tab per AI host and its steps with this server's address. It is on
// the page under the token list and, with the token in it, in the dialog that shows a new token once.
export function ConnectAgent({ host, onHost, origin, mcpAddress, token, hidden }: ConnectAgentProps) {
  const name = useId();
  return <>
    <h3 className="step-title">{text.connectStep}</h3>
    <div className="status-tabs scroll-x-fade host-tabs" role="radiogroup" aria-label={text.host}>
      {connectHosts.map((item) => <label key={item.id}><input type="radio" name={name} value={item.id} checked={host === item.id}
        onChange={() => onHost(item.id)} />{item.name}</label>)}
    </div>
    <HostSteps host={host} origin={origin} mcpAddress={mcpAddress} token={token} hidden={hidden} />
    <p className="muted-copy check-it">{text.checkIt}</p>
  </>;
}

// hidden is a token just created and hidden: the steps say so instead of asking for a first one.
function HostSteps({ host, origin, mcpAddress, token, hidden }: { host: ConnectHostID; origin: string; mcpAddress: string; token?: string; hidden: boolean }) {
  const item = connectHosts.find((candidate) => candidate.id === host)!;
  const snippet = (name: keyof typeof connectSnippets) => connectSnippets[name].replaceAll(exampleOrigin, origin);
  // Claude Code signs in with OAuth: the Colliedesk plugin on Cloud, this server's address elsewhere.
  const plugin = mcpAddress === pluginMCPURL;
  const [before, command] = host === 'claude-code' && !plugin ? text.claudeCodeRemove : text.disconnects[host];
  const after = host === 'claude-code' ? text.disconnectAfter : text.revokeAfter;
  let steps: ReactNode;
  if (host === 'claude-code') {
    steps = <>
      <p className="muted-copy">{text.claudeCodeOAuth}</p>
      <ol className="host-steps">
        {plugin ? <>
          <li><Snippet label={text.runInTerminal} code={connectSnippets.claudeCodePlugin} /><p className="muted-copy">{text.pluginInstalls}</p></li>
          <li><Snippet label={text.thenSignIn} code={connectSnippets.claudeCodeLogin} /><p className="muted-copy">{text.claudeCodeAllow}</p></li>
        </> : <>
          <li><Snippet label={text.runInTerminal} code={connectSnippets.claudeCodeCLI.replaceAll(`${exampleOrigin}/mcp`, mcpAddress)} /></li>
          <li><Snippet label={text.thenSignIn} code={connectSnippets.claudeCodeCLILogin} /><p className="muted-copy">{text.claudeCodeAllow}</p></li>
        </>}
      </ol>
      {!plugin && <p className="muted-copy">{text.claudeCodeSkills}</p>}
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
        {' · '}{text.disconnect}{before}{command && <code>{command}</code>}{after}</p>
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
