import { useEffect, useRef, useState } from 'react';
import { draftFileHref, type AIDraft } from './ai-draft-api';
import { messages } from '../../messages';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';

// files names the draft's files the reply keeps, when the person dropped some.
export interface DraftEdit { body?: string; cc?: string[]; files?: string[] }

interface ReplyDraftCardProps {
  // A written draft (source written): words an agent token, or a person, left for a person to send.
  draft: AIDraft;
  // customer names who the reply goes to.
  customer?: string;
  // onSend sends the draft as a reply: undefined as it was written, an edit with the changed text and
  // only the copies and files the person kept.
  onSend: (edit?: DraftEdit) => Promise<unknown>;
  onDiscard: () => Promise<unknown>;
}

const draftLabel = (draft: AIDraft) => (draft.written_by?.token_label
  ? messages.replyDraftBy(draft.written_by.agent_name || messages.someone, draft.written_by.token_label)
  : messages.replyDraftOf(draft.written_by?.agent_name || messages.someone));

// DraftFiles lists a draft's files as downloads; with onToggle, each has a box that keeps it on the reply.
function DraftFiles({ draft, kept, onToggle }: { draft: AIDraft; kept?: string[]; onToggle?: (id: string, keep: boolean) => void }) {
  if (draft.files.length === 0) return null;
  return (
    <fieldset className="reply-draft-files">
      <legend>{onToggle ? messages.draftFilesKeep : messages.draftFiles}</legend>
      <ul>
        {draft.files.map((file) => <li key={file.id}>
          {onToggle && <input type="checkbox" aria-label={messages.draftFileSend(file.filename)} checked={kept?.includes(file.id) ?? true}
            onChange={(event) => onToggle(file.id, event.target.checked)} />}
          <a href={draftFileHref(draft, file.id)} download={file.filename} title={messages.downloadAttachment(file.filename)}>{file.filename}</a>
          <span className="attachment-size">{formatBytes(file.size)}</span>
        </li>)}
      </ul>
    </fieldset>
  );
}

// ReplyDraftCard is a reply an AI agent (or a person) left on this conversation. Nothing reaches the
// customer until a person presses Send; they may edit it first, drop some of its files, or discard it.
export function ReplyDraftCard({ draft, customer, onSend, onDiscard }: ReplyDraftCardProps) {
  const original = draft.text ?? '';
  const copies = draft.cc ?? [];
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(original);
  // The copies and files the person keeps; the draft's own lists are the most they can be.
  const [kept, setKept] = useState<string[]>(copies);
  const [keptFiles, setKeptFiles] = useState<string[]>(() => draft.files.map((file) => file.id));
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
  const label = draftLabel(draft);
  const edit: DraftEdit = {};
  if (editing && text !== original) edit.body = text;
  if (kept.length !== copies.length) edit.cc = kept;
  if (keptFiles.length !== draft.files.length) edit.files = keptFiles;
  const changed = edit.body !== undefined || edit.cc !== undefined || edit.files !== undefined;
  const toggleFile = (id: string, keep: boolean) =>
    setKeptFiles((current) => (keep ? draft.files.map((file) => file.id).filter((item) => item === id || current.includes(item)) : current.filter((item) => item !== id)));
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
      <DraftFiles draft={draft} kept={keptFiles} onToggle={toggleFile} />
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

const ending = (draft: AIDraft) => {
  if (draft.outcome === 'discarded') return draft.decided_by_name ? messages.draftDiscardedBy(draft.decided_by_name) : messages.draftDiscardedOnClose;
  if (draft.outcome === 'expired') return messages.draftExpired;
  return messages.draftReplaced;
};

interface EarlierDraftsProps {
  // Written drafts replaced by a newer one, expired or discarded, newest first.
  drafts: AIDraft[];
  timezone: string;
  // Without it (no ai_assist:create) the drafts are shown without Bring back.
  onRestore?: (draft: AIDraft) => Promise<unknown>;
  // nextBefore starts the page of older drafts onLoadOlder reads; null or absent when there is none.
  nextBefore?: string | null;
  onLoadOlder?: (before: string) => Promise<{ drafts: AIDraft[]; next: string | null }>;
}

// EarlierDrafts keeps the drafts a newer one replaced (or that expired or were discarded) one click
// away: each says how it ended, shows its text and files, and comes back above the reply box.
export function EarlierDrafts({ drafts, timezone, onRestore, nextBefore, onLoadOlder }: EarlierDraftsProps) {
  const [busy, setBusy] = useState<string>();
  const [failed, setFailed] = useState<string>();
  // Older pages read on demand: the drafts and where the next page starts (undefined: the first page's).
  const [older, setOlder] = useState<AIDraft[]>([]);
  const [cursor, setCursor] = useState<string | null>();
  const [loading, setLoading] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const next = cursor === undefined ? nextBefore ?? null : cursor;
  const shown = [...drafts, ...older.filter((draft) => !drafts.some((item) => item.id === draft.id))];
  if (shown.length === 0 && !next) return null;
  const restore = async (draft: AIDraft) => {
    if (!onRestore) return;
    setBusy(draft.id);
    setFailed(undefined);
    try {
      await onRestore(draft);
      // The pages read before may hold the draft that now waits: read them again when asked.
      setOlder([]);
      setCursor(undefined);
    } catch {
      setFailed(draft.id);
    } finally {
      setBusy(undefined);
    }
  };
  const loadOlder = async () => {
    if (!onLoadOlder || !next) return;
    setLoading(true);
    setLoadFailed(false);
    try {
      const page = await onLoadOlder(next);
      setOlder((current) => [...current, ...page.drafts]);
      setCursor(page.next);
    } catch {
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  };
  return (
    <details className="earlier-drafts">
      <summary>{messages.earlierDrafts(shown.length)}</summary>
      <ul>
        {shown.map((draft) => <li key={draft.id} className="earlier-draft">
          <p className="reply-draft-by">{draftLabel(draft)}</p>
          <p className="earlier-draft-ending">{ending(draft)}{draft.decided_at && <> · <time dateTime={draft.decided_at}>{formatDate(draft.decided_at, timezone)}</time></>}</p>
          <p className="reply-draft-text">{draft.text}</p>
          <DraftFiles draft={draft} />
          {failed === draft.id && <p role="alert" className="error-copy">{messages.draftRestoreFailed}</p>}
          {onRestore && <div className="button-row">
            <button type="button" disabled={busy !== undefined} onClick={() => void restore(draft)}>{messages.bringDraftBack}</button>
          </div>}
        </li>)}
      </ul>
      {loadFailed && <p role="alert" className="error-copy">{messages.olderDraftsFailed}</p>}
      {next && onLoadOlder && <button type="button" className="link-action" disabled={loading} onClick={() => void loadOlder()}>{messages.showOlderDrafts}</button>}
    </details>
  );
}
