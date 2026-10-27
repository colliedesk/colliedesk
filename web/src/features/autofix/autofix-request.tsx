import { useEffect, useRef, useState } from 'react';
import type { AgentRequest } from '../../api/schemas';
import type { FixConnector } from './autofix-api';
import { useAutofixSettings, goTo } from './autofix-conversation';
import { autofixMessages } from './autofix-messages';
import { BodyView } from './send-dialog';

const m = autofixMessages.req;

// The Autofix calls an AI agent's token files as a request (plan §4): sending or retrying a fix, installing the App,
// letting notes go, turning an agent on. The card says in words what approving starts.
const sendRoutes = new Set(['/api/v1/conversations/{id}/fixes', '/api/v1/autofix/fixes/{id}/retry']);

const field = (body: unknown, key: string): unknown => (typeof body === 'object' && body !== null && key in body ? (body as Record<string, unknown>)[key] : undefined);
const text = (body: unknown, key: string) => { const value = field(body, key); return typeof value === 'string' ? value : undefined; };
const connectorOf = (value: unknown): FixConnector | undefined => (value === 'claude_issue' || value === 'copilot_issue' || value === 'cursor_api' ? value : undefined);

// AutofixRequestDetail is the middle of a request card for an Autofix call. Approving one that goes through GitHub
// answers where the approver's browser goes (authorize_url, shown to them once and never to the AI agent): it goes.
export function AutofixRequestDetail({ item, result, onApprove, busy = false }: { item: AgentRequest; result?: unknown; onApprove?: () => void; busy?: boolean }) {
  const settings = useAutofixSettings();
  const [agreed, setAgreed] = useState(false);
  const left = useRef(false);
  const [refused, setRefused] = useState<string>();
  const url = text(result, 'authorize_url');
  useEffect(() => {
    if (!url || left.current) return;
    left.current = true;
    try {
      goTo(url);
    } catch (error) {
      if (error instanceof Error) setRefused(error.message);
    }
  }, [url]);
  const refusal = refused && <p role="alert" className="error-copy">{refused}</p>;
  const body = item.edited_body ?? item.body;
  // What approving would do is said while it is still to be decided; a decided request is past that.
  const pending = item.status === 'pending';
  if (sendRoutes.has(item.route)) {
    const connector = connectorOf(field(body, 'connector'));
    const repo = settings.data?.repos.find((candidate) => candidate.id === field(body, 'repo_id'));
    const notes = field(body, 'note_ids');
    const sentText = text(body, 'body');
    return <>
      <dl className="af-req-what">
        {repo && <div><dt>{m.repository}</dt><dd className="af-mono">{repo.full_name}</dd></div>}
        {connector && <div><dt>{m.agent}</dt><dd>{autofixMessages.vendor(connector)} · {autofixMessages.agentPick[connector]}</dd></div>}
        {text(body, 'title') && <div><dt>{m.title}</dt><dd>{text(body, 'title')}</dd></div>}
        {sentText && <div><dt>{m.text}</dt><dd><BodyView body={sentText} /></dd></div>}
        <div><dt>{m.notes}</dt><dd>{Array.isArray(notes) && notes.length > 0 ? m.notesN(notes.length) : m.none}</dd></div>
      </dl>
      {pending && (connector === 'cursor_api' ? <div className="af-oc af-note"><span>{m.cursorNow}</span></div>
        : <div className="af-oc"><span><b>{m.becomeSender}</b> {m.becomeSenderHelp}</span></div>)}
      {refusal}
    </>;
  }
  if (item.route === '/api/v1/autofix/github/installations') return <>{pending && <div className="af-oc"><span>{m.installHelp}</span></div>}{refusal}</>;
  if (item.route === '/api/v1/autofix/github/installations/{id}' && item.method === 'DELETE') {
    if (!pending) return null;
    const installation = settings.data?.installations.find((candidate) => candidate.id === item.path.split('/').pop());
    const repos = settings.data?.repos.filter((repo) => repo.installation_id === installation?.installation_id).length ?? 0;
    const set = autofixMessages.set;
    return <div className="af-oc af-danger"><span><b>{set.disconnectTitle}</b> {installation && `${set.disconnectHelp(installation.account_login, repos)} `}{set.disconnectNote} {set.disconnectAgain}</span></div>;
  }
  if (item.route === '/api/v1/autofix/repos/{id}/include-notes') return <p className="af-help">{autofixMessages.set.notesHelp}</p>;
  if (item.route === '/api/v1/autofix/connectors/{kind}' && item.method === 'PUT') {
    const kind = connectorOf(item.path.split('/').pop());
    if (!kind) return null;
    // The consent text changed since the AI agent asked: approving would be refused (409 consent_outdated).
    const outdated = settings.data !== undefined && field(body, 'consent_version') !== settings.data.consent_version;
    // Data consent is a deliberate act of the person approving: Approve is here, and only once they tick it.
    return onApprove ? <div className="af-req-consent">
      <label className="af-consent"><input type="checkbox" checked={agreed} onChange={(event) => setAgreed(event.target.checked)} /><span>{autofixMessages.set.consent[kind]}</span></label>
      {outdated && <p role="alert" className="error-copy">{m.consentOutdated}</p>}
      <div className="af-acts"><button type="button" className="primary-action" disabled={!agreed || busy || outdated} onClick={onApprove}>{m.approve}</button>
        {!agreed && <span className="af-help">{m.agreeFirst}</span>}</div>
    </div> : <div className="af-oc af-note"><span><b>{m.consentNote}</b> {autofixMessages.set.consent[kind]}</span></div>;
  }
  return null;
}
