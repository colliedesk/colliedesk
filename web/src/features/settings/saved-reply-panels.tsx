import { useCallback, useEffect, useRef, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { Generated, SavedReplyChange } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';

const day = 24 * 60 * 60 * 1000;
// A version text longer than this is folded until the person asks for all of it.
const foldAfter = 280;

// usePages loads the first page when the panel opens and again after reload(), and appends the next
// page on more(). The newest load is used, so callers may pass an inline function.
function usePages<T, Cursor>(load: (cursor?: Cursor) => Promise<{ items: T[]; next?: Cursor }>) {
  const [items, setItems] = useState<T[]>();
  const [next, setNext] = useState<Cursor>();
  const [failed, setFailed] = useState(false);
  const latest = useRef(load);
  latest.current = load;
  const fetchPage = useCallback((cursor: Cursor | undefined, append: boolean) => {
    setFailed(false);
    latest.current(cursor).then((page) => {
      setItems((current) => (append ? [...(current ?? []), ...page.items] : page.items));
      setNext(page.next);
    }, () => setFailed(true));
  }, []);
  const reload = useCallback(() => fetchPage(undefined, false), [fetchPage]);
  useEffect(() => { reload(); }, [reload]);
  return { items, failed, reload, more: next === undefined ? undefined : () => fetchPage(next, true) };
}

// useAction runs one action at a time and keeps the server's reason when it is refused.
function useAction(failedMessage: string) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const run = async (action: () => Promise<unknown>, done: () => void) => {
    setBusy(true);
    setProblem(undefined);
    try {
      await action();
      done();
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : failedMessage);
    } finally {
      setBusy(false);
    }
  };
  return { busy, problem, run };
}

// FoldedText shows a long text cut, with a toggle for the rest.
function FoldedText({ text, label }: { text: string; label: string }) {
  const [open, setOpen] = useState(false);
  if (text.length <= foldAfter) return <span className="saved-reply-version-body">{text}</span>;
  return <>
    <span className="saved-reply-version-body">{open ? text : `${text.slice(0, foldAfter)}…`}</span>
    <button type="button" className="link-button" aria-expanded={open} aria-label={label} onClick={() => setOpen(!open)}>
      {open ? messages.showLessText : messages.showWholeText}</button>
  </>;
}

// A version as the history shows it: who changed what, and the title and text after the change. Saved
// replies and knowledge base articles both have one.
export type HistoryVersion = Pick<SavedReplyChange, 'version' | 'restored_from_version' | 'changed_by' | 'token' | 'agent_request_id' | 'created_at'>
  & { kind: SavedReplyChange['kind'] | 'import' | 'media_replace'; title: string };

interface HistoryProps<V extends HistoryVersion> {
  title: string;
  timezone: string;
  load: (beforeVersion?: number) => Promise<{ items: V[]; next_before_version: number | null }>;
  // Makes version the current text; resolves once the server saved it.
  onRestore: (version: number) => Promise<unknown>;
  onClose: () => void;
  // text is the version's text; a saved reply's body by default.
  text?: (version: V) => string;
  // changes names what a version changed from the one before it; title and text by default.
  changes?: (version: V, previous: V) => string[];
  // failedMessage stands in for a refusal that has no reason of its own.
  failedMessage?: string;
}

const replyText = (version: HistoryVersion) => ('body' in version && typeof version.body === 'string' ? version.body : '');

// SavedReplyHistory lists the versions of one reply (or article), newest first, a page at a time, with
// what each changed and Restore on the older ones.
export function SavedReplyHistory<V extends HistoryVersion>({ title, timezone, load, onRestore, onClose, text = replyText, changes, failedMessage = messages.savedReplyActionFailed }: HistoryProps<V>) {
  const { items, failed, reload, more } = usePages<V, number>(async (before) => {
    const page = await load(before);
    return { items: page.items, next: page.next_before_version ?? undefined };
  });
  const { busy, problem, run } = useAction(failedMessage);
  const changed = changes ?? ((version: V, previous: V) => [
    ...(previous.title !== version.title ? [messages.savedReplyTitleChanged] : []),
    ...(text(previous) !== text(version) ? [messages.savedReplyTextChanged] : []),
  ]);
  const name = messages.savedReplyHistoryOf(title);
  return (
    <section className="saved-reply-panel" aria-label={name}>
      <div className="saved-reply-panel-head">
        <strong>{name}</strong>
        <button type="button" onClick={onClose}>{messages.closeSavedReplyHistory}</button>
      </div>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {failed && <p className="error-copy">{messages.savedReplyPanelFailed}</p>}
      {!items ? !failed && <p className="muted-copy">{messages.loadingSavedReplyHistory}</p>
        : <ol className="saved-reply-versions">{items.map((version, index) => {
          const previous = items[index + 1];
          return (
            <li key={version.version}>
              <div className="saved-reply-text">
                <strong>{messages.savedReplyVersion(version.version)} · {version.title}</strong>
                <span className="saved-reply-meta">{savedReplyChangeText(version)} · <time dateTime={version.created_at}>{formatDate(version.created_at, timezone)}</time>
                  {previous && changed(version, previous).map((change) => <span key={change} className="saved-reply-tag">{change}</span>)}</span>
                <FoldedText text={text(version)} label={messages.showWholeVersion(version.version)} />
              </div>
              {index > 0 && <button type="button" disabled={busy} onClick={() => void run(() => onRestore(version.version), reload)}>
                {messages.restoreSavedReplyVersion(version.version)}</button>}
            </li>
          );
        })}</ol>}
      {more && <button type="button" className="saved-reply-more" onClick={more}>{messages.showOlderSavedReplyVersions}</button>}
    </section>
  );
}

// An item in a trash: a saved reply, or a knowledge base article or collection.
export interface TrashedItem {
  id: string;
  title: string;
  updated_at: string;
  deleted_at: string;
  deleted_by: Generated<'SavedReplyPerson'> | null;
  purges_at: string;
}

interface TrashProps<T extends TrashedItem> {
  timezone: string;
  now: Date;
  // A page of the trash; next_cursor is null on the last page. An endpoint without pages leaves it out.
  load: (cursor?: string) => Promise<{ items: T[]; next_cursor?: string | null }>;
  // updatedAt is the version of the reply the trash shows; the server refuses it if it changed since.
  onUndelete: (id: string, updatedAt: string) => Promise<unknown>;
  // Without onClose the panel has no close button: it sits in a larger panel that has one.
  onClose?: () => void;
  // The panel's name and copy; the saved replies' by default.
  heading?: string;
  description?: string;
  empty?: string;
  closeLabel?: string;
  // text is the line under the title; a saved reply's body by default, nothing when it returns ''.
  text?: (item: T) => string;
  failedMessage?: string;
}

const trashedReplyText = (item: TrashedItem) => ('body' in item && typeof item.body === 'string' ? item.body : '');

// SavedReplyTrash lists the deleted replies (or articles, or collections), a page at a time, with the
// days left before they are deleted for good.
export function SavedReplyTrash<T extends TrashedItem>({ timezone, now, load, onUndelete, onClose, heading = messages.savedReplyTrash,
  description = messages.savedReplyTrashDescription, empty = messages.savedReplyTrashEmpty, closeLabel = messages.closeSavedReplyTrash,
  text = trashedReplyText, failedMessage = messages.savedReplyActionFailed }: TrashProps<T>) {
  const { items, failed, reload, more } = usePages<T, string>(async (cursor) => {
    const page = await load(cursor);
    return { items: page.items, next: page.next_cursor ?? undefined };
  });
  const { busy, problem, run } = useAction(failedMessage);
  return (
    <section className="saved-reply-panel" aria-label={heading}>
      <div className="saved-reply-panel-head">
        <strong>{heading}</strong>
        {onClose && <button type="button" onClick={onClose}>{closeLabel}</button>}
      </div>
      <p className="muted-copy">{description}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {failed && <p className="error-copy">{messages.savedReplyPanelFailed}</p>}
      {!items ? !failed && <p className="muted-copy">{messages.loadingSavedReplyHistory}</p>
        : items.length === 0 ? <p className="empty-copy">{empty}</p>
          : <ul className="saved-reply-versions">{items.map((reply) => (
            <li key={reply.id}>
              <div className="saved-reply-text">
                <strong>{reply.title}</strong>
                {text(reply) && <span>{text(reply)}</span>}
                <span className="saved-reply-meta">{messages.savedReplyDeletedBy(reply.deleted_by?.name ?? messages.someone)} · <time dateTime={reply.deleted_at}>{formatDate(reply.deleted_at, timezone)}</time>
                  {' · '}{messages.savedReplyDaysLeft(Math.max(0, Math.ceil((Date.parse(reply.purges_at) - now.getTime()) / day)))}</span>
              </div>
              <button type="button" disabled={busy} aria-label={messages.restoreSavedReplyName(reply.title)}
                onClick={() => void run(() => onUndelete(reply.id, reply.updated_at), reload)}>{messages.restoreSavedReply}</button>
            </li>
          ))}</ul>}
      {more && <button type="button" className="saved-reply-more" onClick={more}>{messages.showMoreTrash}</button>}
    </section>
  );
}
