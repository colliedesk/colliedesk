import { useCallback, useEffect, useRef, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { SavedReplyVersion, SavedReplyVersionPage, TrashedSavedReply, TrashedSavedReplyPage } from '../../api/schemas';
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
function useAction() {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const run = async (action: () => Promise<unknown>, done: () => void) => {
    setBusy(true);
    setProblem(undefined);
    try {
      await action();
      done();
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : messages.savedReplyActionFailed);
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

interface HistoryProps {
  title: string;
  timezone: string;
  load: (beforeVersion?: number) => Promise<SavedReplyVersionPage>;
  // Makes version the current text; resolves once the server saved it.
  onRestore: (version: number) => Promise<unknown>;
  onClose: () => void;
}

// SavedReplyHistory lists the versions of one reply, newest first, a page at a time, with what each
// changed and Restore on the older ones.
export function SavedReplyHistory({ title, timezone, load, onRestore, onClose }: HistoryProps) {
  const { items, failed, reload, more } = usePages<SavedReplyVersion, number>(async (before) => {
    const page = await load(before);
    return { items: page.items, next: page.next_before_version ?? undefined };
  });
  const { busy, problem, run } = useAction();
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
                  {previous && previous.title !== version.title && <span className="saved-reply-tag">{messages.savedReplyTitleChanged}</span>}
                  {previous && previous.body !== version.body && <span className="saved-reply-tag">{messages.savedReplyTextChanged}</span>}</span>
                <FoldedText text={version.body} label={messages.showWholeVersion(version.version)} />
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

interface TrashProps {
  timezone: string;
  now: Date;
  load: (cursor?: string) => Promise<TrashedSavedReplyPage>;
  // updatedAt is the version of the reply the trash shows; the server refuses it if it changed since.
  onUndelete: (id: string, updatedAt: string) => Promise<unknown>;
  onClose: () => void;
}

// SavedReplyTrash lists the deleted replies, a page at a time, with the days left before they are
// deleted for good.
export function SavedReplyTrash({ timezone, now, load, onUndelete, onClose }: TrashProps) {
  const { items, failed, reload, more } = usePages<TrashedSavedReply, string>(async (cursor) => {
    const page = await load(cursor);
    return { items: page.items, next: page.next_cursor ?? undefined };
  });
  const { busy, problem, run } = useAction();
  return (
    <section className="saved-reply-panel" aria-label={messages.savedReplyTrash}>
      <div className="saved-reply-panel-head">
        <strong>{messages.savedReplyTrash}</strong>
        <button type="button" onClick={onClose}>{messages.closeSavedReplyTrash}</button>
      </div>
      <p className="muted-copy">{messages.savedReplyTrashDescription}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {failed && <p className="error-copy">{messages.savedReplyPanelFailed}</p>}
      {!items ? !failed && <p className="muted-copy">{messages.loadingSavedReplyHistory}</p>
        : items.length === 0 ? <p className="empty-copy">{messages.savedReplyTrashEmpty}</p>
          : <ul className="saved-reply-versions">{items.map((reply) => (
            <li key={reply.id}>
              <div className="saved-reply-text">
                <strong>{reply.title}</strong>
                <span>{reply.body}</span>
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
