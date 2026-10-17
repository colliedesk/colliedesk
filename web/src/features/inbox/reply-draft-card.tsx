import { useEffect, useRef, useState } from 'react';
import type { AgentRequest } from '../../api/schemas';
import { messages } from '../../messages';
import { replyCall } from '../requests/request-call';

export interface DraftEdit { body?: string; cc?: string[] }

interface ReplyDraftCardProps {
  draft: AgentRequest;
  // customer names who the reply goes to.
  customer?: string;
  // onSend approves the draft: undefined sends it as the agent wrote it, an edit sends the changed
  // text and only the copies the person kept.
  onSend: (edit?: DraftEdit) => Promise<unknown>;
  onDiscard: () => Promise<unknown>;
}

// ReplyDraftCard is a reply an AI agent drafted on this conversation. Nothing reaches the customer
// until a person presses Send; they may edit it first or discard it.
export function ReplyDraftCard({ draft, customer, onSend, onDiscard }: ReplyDraftCardProps) {
  const call = replyCall(draft.body);
  const original = call?.text ?? '';
  const copies = call?.cc ?? [];
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(original);
  // The copies the person keeps; the draft's own list is the most it can be.
  const [kept, setKept] = useState<string[]>(copies);
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
  const edit: DraftEdit = {};
  if (editing && text !== original) edit.body = text;
  if (kept.length !== copies.length) edit.cc = kept;
  const changed = edit.body !== undefined || edit.cc !== undefined;
  return (
    <section className="reply-draft" aria-label={label}>
      <p className="reply-draft-by">{label}</p>
      <div className="reply-draft-recipients"><span>{messages.draftTo(customer ?? messages.theCustomer)}</span>
        {copies.length > 0 && !editing && <span>{messages.draftCc(copies)}</span>}</div>
      {editing && copies.length > 0 && <fieldset className="reply-draft-copies"><legend>{messages.draftCopies}</legend>
        {copies.map((address) => <label key={address} className="field-inline">
          <input type="checkbox" aria-label={messages.draftCopy(address)} checked={kept.includes(address)}
            onChange={(event) => setKept(event.target.checked ? copies.filter((item) => item === address || kept.includes(item)) : kept.filter((item) => item !== address))} />
          <span>{address}</span></label>)}
      </fieldset>}
      {editing
        ? <textarea ref={field} className="reply-draft-edit" aria-label={messages.draftText} rows={4} value={text} maxLength={20000} onChange={(event) => setText(event.target.value)} />
        : <p className="reply-draft-text">{original}</p>}
      {failed && <p role="alert" className="error-copy">{messages.draftSendFailed}</p>}
      <div className="button-row">
        <button type="button" className="primary-action" disabled={busy || (editing && !text.trim())}
          onClick={() => void run(() => onSend(changed ? edit : undefined))}>{messages.sendDraft}</button>
        {!editing && <button type="button" disabled={busy} onClick={() => setEditing(true)}>{messages.edit}</button>}
        <button type="button" className="danger-action" disabled={busy} onClick={() => void run(onDiscard)}>{messages.discardDraft}</button>
      </div>
    </section>
  );
}
