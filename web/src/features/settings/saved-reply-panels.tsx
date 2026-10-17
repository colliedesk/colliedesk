import { useCallback, useEffect, useRef, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { SavedReplyVersion, TrashedSavedReply } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';

const day = 24 * 60 * 60 * 1000;

// useLoaded runs load when the panel opens and again after reload(), keeping the last answer and
// whether it failed. The newest load is used, so callers may pass an inline function.
function useLoaded<T>(load: () => Promise<T>) {
  const [data, setData] = useState<T>();
  const [failed, setFailed] = useState(false);
  const latest = useRef(load);
  latest.current = load;
  const reload = useCallback(() => {
    setFailed(false);
    latest.current().then(setData, () => setFailed(true));
  }, []);
  useEffect(() => { reload(); }, [reload]);
  return { data, failed, reload };
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

interface HistoryProps {
  title: string;
  timezone: string;
  load: () => Promise<SavedReplyVersion[]>;
  // Makes version the current text; resolves once the server saved it.
  onRestore: (version: number) => Promise<unknown>;
  onClose: () => void;
}

// SavedReplyHistory lists every version of one reply, newest first, with Restore on the older ones.
export function SavedReplyHistory({ title, timezone, load, onRestore, onClose }: HistoryProps) {
  const { data, failed, reload } = useLoaded(load);
  const { busy, problem, run } = useAction();
  const name = messages.savedReplyHistoryOf(title);
  return (
    <section className="saved-reply-panel" aria-label={name}>
      <div className="saved-reply-panel-head">
        <strong>{name}</strong>
        <button type="button" onClick={onClose}>{messages.closeSavedReplyHistory}</button>
      </div>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {failed ? <p className="error-copy">{messages.savedReplyPanelFailed}</p>
        : !data ? <p className="muted-copy">{messages.loadingSavedReplyHistory}</p>
          : <ol className="saved-reply-versions">{data.map((version, index) => (
            <li key={version.version}>
              <div className="saved-reply-text">
                <strong>{messages.savedReplyVersion(version.version)} · {version.title}</strong>
                <span className="muted-copy">{savedReplyChangeText(version)} · <time dateTime={version.created_at}>{formatDate(version.created_at, timezone)}</time></span>
                <span className="saved-reply-version-body">{version.body}</span>
              </div>
              {index > 0 && <button type="button" disabled={busy} onClick={() => void run(() => onRestore(version.version), reload)}>
                {messages.restoreSavedReplyVersion(version.version)}</button>}
            </li>
          ))}</ol>}
    </section>
  );
}

interface TrashProps {
  timezone: string;
  now: Date;
  load: () => Promise<TrashedSavedReply[]>;
  onUndelete: (id: string) => Promise<unknown>;
  onClose: () => void;
}

// SavedReplyTrash lists the deleted replies with the days left before they are deleted for good.
export function SavedReplyTrash({ timezone, now, load, onUndelete, onClose }: TrashProps) {
  const { data, failed, reload } = useLoaded(load);
  const { busy, problem, run } = useAction();
  return (
    <section className="saved-reply-panel" aria-label={messages.savedReplyTrash}>
      <div className="saved-reply-panel-head">
        <strong>{messages.savedReplyTrash}</strong>
        <button type="button" onClick={onClose}>{messages.closeSavedReplyTrash}</button>
      </div>
      <p className="muted-copy">{messages.savedReplyTrashDescription}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {failed ? <p className="error-copy">{messages.savedReplyPanelFailed}</p>
        : !data ? <p className="muted-copy">{messages.loadingSavedReplyHistory}</p>
          : data.length === 0 ? <p className="empty-copy">{messages.savedReplyTrashEmpty}</p>
            : <ul className="saved-reply-versions">{data.map((reply) => (
              <li key={reply.id}>
                <div className="saved-reply-text">
                  <strong>{reply.title}</strong>
                  <span>{reply.body}</span>
                  <span className="muted-copy">{messages.savedReplyDeletedBy(reply.deleted_by?.name ?? messages.someone)} · <time dateTime={reply.deleted_at}>{formatDate(reply.deleted_at, timezone)}</time>
                    {' · '}{messages.savedReplyDaysLeft(Math.max(0, Math.ceil((Date.parse(reply.purges_at) - now.getTime()) / day)))}</span>
                </div>
                <button type="button" disabled={busy} aria-label={messages.restoreSavedReplyName(reply.title)}
                  onClick={() => void run(() => onUndelete(reply.id), reload)}>{messages.restoreSavedReply}</button>
              </li>
            ))}</ul>}
    </section>
  );
}
