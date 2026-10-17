import { useEffect, useRef, useState } from 'react';
import type { AgentRequest } from '../../api/schemas';
import { messages } from '../../messages';

interface ReplyDraftCardProps {
  draft: AgentRequest;
  // onSend approves the draft: undefined sends it as the agent wrote it, a string sends the edit.
  onSend: (body?: string) => Promise<unknown>;
  onDiscard: () => Promise<unknown>;
}

const draftText = (draft: AgentRequest) =>
  typeof draft.body === 'object' && draft.body !== null && 'body' in draft.body && typeof draft.body.body === 'string' ? draft.body.body : '';

// ReplyDraftCard is a reply an AI agent drafted on this conversation. Nothing reaches the customer
// until a person presses Send; they may edit it first or discard it.
export function ReplyDraftCard({ draft, onSend, onDiscard }: ReplyDraftCardProps) {
  const original = draftText(draft);
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(original);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (editing) field.current?.focus(); }, [editing]);
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setFailed(false);
    try {
      await action();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const label = messages.replyDraftBy(draft.requested_by.agent_name ?? messages.someone, draft.requested_by.token_label);
  const edited = editing && text !== original;
  return (
    <section className="reply-draft" aria-label={label}>
      <p className="reply-draft-by">{label}</p>
      {editing
        ? <textarea ref={field} className="reply-draft-edit" aria-label={messages.draftText} rows={4} value={text} maxLength={20000} onChange={(event) => setText(event.target.value)} />
        : <p className="reply-draft-text">{original}</p>}
      {failed && <p role="alert" className="error-copy">{messages.draftSendFailed}</p>}
      <div className="button-row">
        <button type="button" className="primary-action" disabled={busy || (editing && !text.trim())}
          onClick={() => void run(() => onSend(edited ? text : undefined))}>{messages.sendDraft}</button>
        {!editing && <button type="button" disabled={busy} onClick={() => setEditing(true)}>{messages.edit}</button>}
        <button type="button" className="danger-action" disabled={busy} onClick={() => void run(onDiscard)}>{messages.discardDraft}</button>
      </div>
    </section>
  );
}
