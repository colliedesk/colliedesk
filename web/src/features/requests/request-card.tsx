import { useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { AgentRequest } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { track } from '../../lib/product-analytics';
import { permissionLabel } from '../settings/token-permissions';
import { messages } from '../../messages';
import { PartUnavailable } from '../../ui/lazy-part';
import { gmailPart } from '../settings/email-channel-section';
import { replyCall } from './request-call';
import { isSavedReplyRequest, SavedReplyProposal } from './saved-reply-proposal';

export interface ApproveOutcome { request: AgentRequest; result: unknown }

interface RequestCardProps {
  item: AgentRequest;
  timezone: string;
  // Each action resolves when the server decided and rejects with the reason when it refused.
  onApprove: (id: string) => Promise<ApproveOutcome>;
  onReject: (id: string, reason?: string) => Promise<unknown>;
  // canApprove is false when the signed-in person's role cannot run the call; only Reject is offered.
  canApprove?: boolean;
  // initialOutcome is an approval made a moment ago, kept on screen after the list moved on.
  initialOutcome?: ApproveOutcome;
}

const resultURL = (result: unknown) =>
  typeof result === 'object' && result !== null && 'url' in result && typeof result.url === 'string' ? result.url : undefined;

// Approving a Gmail connection answers Google's consent link, so the Gmail block's disclosure comes first. It
// lives in that block's chunk; the card for such a request loads it while the request is still pending.
function GmailDisclosure() {
  const gmail = gmailPart.use();
  return gmail.status === 'ready' ? <gmail.module.GmailDataDisclosure /> : gmail.status === 'error' && <PartUnavailable />;
}

// RequestCard is one agent request: what it does, who asked, the exact call and, while it waits,
// Approve and Reject. A credential or link the approved call answered is shown here once.
export function RequestCard({ item, timezone, onApprove, onReject, canApprove = true, initialOutcome }: RequestCardProps) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [outcome, setOutcome] = useState<ApproveOutcome | undefined>(initialOutcome);
  const current = outcome?.request ?? item;
  const headingId = `request-${item.id}`;
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(undefined);
    try {
      await action();
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : messages.requestActionFailed);
    } finally {
      setBusy(false);
    }
  };
  const person = current.requested_by.agent_name ?? messages.someone;
  const url = resultURL(outcome?.result);
  const reply = current.kind === 'reply_draft' ? replyCall(current.body) : undefined;
  const edited = current.kind === 'reply_draft' ? replyCall(current.edited_body) : undefined;
  return (
    <article className="request-card" aria-labelledby={headingId} data-status={current.status}>
      <header className="request-head">
        <h3 id={headingId}>{current.summary}</h3>
        <span className={`badge badge-request-${current.status}`}>{messages.requestStatus[current.status]}</span>
      </header>
      <p className="muted-copy">{messages.requestedBy(person, current.requested_by.token_label, current.requested_by.token_scope)}</p>
      {/* A custom token's own list, as it was when it asked: what the person approving extends. */}
      {current.requested_by.token_scope === 'custom' && current.requested_by.token_permissions.length > 0 && <details className="token-permissions">
        <summary>{messages.tokenHeld(current.requested_by.token_permissions.length)}</summary>
        <ul>{current.requested_by.token_permissions.map((name) => <li key={name}>{permissionLabel(name)}</li>)}</ul></details>}
      <dl className="request-call">
        <dt>{messages.requestCall}</dt><dd><code>{`${current.method} ${current.path}`}</code></dd>
      </dl>
      {reply
        ? <>
            <p className="request-body reply-draft-text">{reply.text}</p>
            {reply.cc.length > 0 && <p className="request-recipients">{messages.draftCc(reply.cc)}</p>}
          </>
        : isSavedReplyRequest(current) ? <SavedReplyProposal item={current} />
          : current.body !== null && <pre className="request-body">{JSON.stringify(current.body, null, 2)}</pre>}
      {edited && <>
        <p className="muted-copy">{messages.editedBy(current.decided_by?.name ?? messages.someone)}</p>
        <p className="request-body reply-draft-text">{edited.text}</p>
        {edited.cc.length > 0 && <p className="request-recipients">{messages.draftCc(edited.cc)}</p>}
      </>}
      <p className="muted-copy"><time dateTime={current.created_at}>{formatDate(current.created_at, timezone)}</time>
        {current.status === 'pending' && <> · {messages.requestExpires(formatDate(current.expires_at, timezone))}</>}</p>
      {current.method === 'POST' && current.route === '/api/v1/channels/email/gmail' && <GmailDisclosure />}
      {current.status !== 'pending' && current.decided_by && <p>{messages.requestOutcome(messages.requestDecisionVerb[current.status], current.decided_by.name, current.result_status)}</p>}
      {current.reason && <p className="muted-copy">{current.reason}</p>}
      {current.result_note && !outcome && <p className="muted-copy">{current.result_note}</p>}
      {outcome && outcome.result !== null && <div className="request-result" role="status">
        <p>{messages.requestResultOnce}</p>
        {url ? <a href={url} target="_blank" rel="noreferrer">{messages.openResult(url)}</a> : <pre className="request-body">{JSON.stringify(outcome.result, null, 2)}</pre>}
      </div>}
      {current.requested_by.token_revoked && current.status === 'pending' && <p className="warning-note">{messages.tokenRevoked}</p>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {current.status === 'pending' && <div className="request-actions">
        {canApprove
          ? <button type="button" className="primary-action" disabled={busy || current.requested_by.token_revoked}
              onClick={() => void run(async () => { setOutcome(await onApprove(item.id)); track('agent_request_decided', { decision: 'approved' }); })}>{messages.approve}</button>
          : <span className="muted-copy">{item.approvable_by === 'owner' ? messages.onlyOwnerApproves : messages.onlyAdminApproves}</span>}
        <label className="request-reason"><span>{messages.rejectReason}</span>
          <input value={reason} maxLength={1000} onChange={(event) => setReason(event.target.value)} /></label>
        <button type="button" className="danger-action" disabled={busy}
          onClick={() => void run(async () => { await onReject(item.id, reason.trim() || undefined); track('agent_request_decided', { decision: 'rejected' }); })}>{messages.reject}</button>
      </div>}
    </article>
  );
}
