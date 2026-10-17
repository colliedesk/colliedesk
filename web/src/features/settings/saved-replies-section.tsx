import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import type { SavedReply, SavedReplyVersion, TrashedSavedReply } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';

// History and the trash are opened rarely, so their code loads only when one is opened.
const SavedReplyHistory = lazy(() => import('./saved-reply-panels').then((module) => ({ default: module.SavedReplyHistory })));
const SavedReplyTrash = lazy(() => import('./saved-reply-panels').then((module) => ({ default: module.SavedReplyTrash })));

interface SavedRepliesSectionProps {
  replies: SavedReply[];
  // Each action resolves when the server accepted it and rejects with the reason when it did not.
  onCreate: (title: string, body: string) => Promise<unknown>;
  // updated_at is the version this form was opened on; the server refuses the edit if the reply was saved since.
  onUpdate: (id: string, change: { title: string; body: string; updated_at: string }) => Promise<unknown>;
  onDelete: (id: string) => Promise<unknown>;
  // Loads the list again; used after an edit was refused because someone else saved first.
  onReload?: () => void;
  loadHistory: (id: string) => Promise<SavedReplyVersion[]>;
  // Makes an earlier version current; updated_at is the version of the reply the list shows.
  onRestoreVersion: (id: string, version: number, updatedAt: string) => Promise<unknown>;
  loadTrash: () => Promise<TrashedSavedReply[]>;
  onUndelete: (id: string) => Promise<unknown>;
  timezone?: string;
  // now dates the days left in the trash; tests fix it.
  now?: Date;
}

// Every agent manages the shared saved replies, so nothing here depends on the role.
export function SavedRepliesSection({ replies, onCreate, onUpdate, onDelete, onReload, loadHistory, onRestoreVersion, loadTrash, onUndelete, timezone = 'UTC', now }: SavedRepliesSectionProps) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [editing, setEditing] = useState<{ id: string; title: string; body: string } | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  // The add form is closed until asked for, so the list is what the section shows first.
  const [adding, setAdding] = useState(false);
  // Closing the form (added or cancelled) puts focus back on the button that opened it.
  const newButton = useRef<HTMLButtonElement>(null);
  const [returnFocus, setReturnFocus] = useState(false);
  useEffect(() => { if (returnFocus && !adding) { newButton.current?.focus(); setReturnFocus(false); } }, [returnFocus, adding]);
  const closeAdd = () => { setAdding(false); setReturnFocus(true); };
  // One panel at a time: a reply's history (its id) or the trash. Closing one puts focus back on its button.
  const [panel, setPanel] = useState<string | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const openPanel = (name: string, button: HTMLButtonElement) => { opener.current = button; setPanel(name); };
  const closePanel = () => { setPanel(null); opener.current?.focus(); };
  // run keeps the form open with the server's reason when an action is refused.
  const run = async (action: () => Promise<unknown>, done: () => void) => {
    setBusy(true);
    setProblem(undefined);
    setStale(false);
    try {
      await action();
      done();
    } catch (error) {
      setStale(error instanceof ChangedElsewhereError);
      setProblem(error instanceof InvalidRequestError ? error.message : messages.savedReplyActionFailed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="settings-card" aria-labelledby="saved-replies-heading">
      <div className="section-heading"><h2 id="saved-replies-heading">{messages.savedReplies}</h2><p>{messages.savedRepliesDescription}</p></div>
      <div className="settings-body">
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {stale && <button type="button" onClick={() => { setEditing(null); setProblem(undefined); setStale(false); onReload?.(); }}>{messages.reload}</button>}
        {!editing && !adding && <button type="button" ref={newButton} className="new-saved-reply" onClick={() => { setProblem(undefined); setAdding(true); }}>{messages.newSavedReply}</button>}
        {!editing && adding && <form className="saved-reply-form" aria-label={messages.addSavedReply}
          onSubmit={(event) => { event.preventDefault(); if (!busy) void run(() => onCreate(title, body), () => { setTitle(''); setBody(''); closeAdd(); }); }}>
          <label className="field"><span>{messages.savedReplyTitle}</span><input autoFocus value={title} maxLength={200} onChange={(event) => setTitle(event.target.value)} /></label>
          <label className="field"><span>{messages.savedReplyText}</span><textarea rows={4} value={body} maxLength={20000} onChange={(event) => setBody(event.target.value)} /></label>
          <div className="saved-reply-actions">
            <button className="primary-action" type="submit" disabled={busy || !title.trim() || !body.trim()}>{messages.addSavedReply}</button>
            <button type="button" disabled={busy} onClick={() => { closeAdd(); setProblem(undefined); }}>{messages.cancel}</button>
          </div>
        </form>}
        {replies.length === 0 ? <p className="empty-copy">{messages.noSavedReplies}</p> : <ul className="saved-reply-list">{replies.map((reply) => (
          <li key={reply.id}>
            {editing?.id === reply.id
              ? <form className="saved-reply-form" aria-label={messages.editSavedReplyName(reply.title)}
                  onSubmit={(event) => { event.preventDefault(); if (!busy) void run(() => onUpdate(reply.id, { title: editing.title, body: editing.body, updated_at: reply.updated_at }), () => setEditing(null)); }}>
                  <label className="field"><span>{messages.savedReplyTitle}</span><input value={editing.title} maxLength={200} onChange={(event) => setEditing({ ...editing, title: event.target.value })} /></label>
                  <label className="field"><span>{messages.savedReplyText}</span><textarea rows={4} value={editing.body} maxLength={20000} onChange={(event) => setEditing({ ...editing, body: event.target.value })} /></label>
                  <div className="saved-reply-actions">
                    <button className="primary-action" type="submit" disabled={busy || !editing.title.trim() || !editing.body.trim()}>{messages.saveSavedReply}</button>
                    <button type="button" disabled={busy} onClick={() => { setEditing(null); setProblem(undefined); }}>{messages.cancel}</button>
                  </div>
                </form>
              : <>
                  <div className="saved-reply-text"><strong>{reply.title}</strong><span>{reply.body}</span>
                    {reply.last_change && <small className="muted-copy">{savedReplyChangeText(reply.last_change)} · <time dateTime={reply.last_change.created_at}>{formatDate(reply.last_change.created_at, timezone)}</time></small>}</div>
                  {confirming === reply.id
                    ? <div role="group" aria-label={messages.deleteSavedReplyName(reply.title)} className="saved-reply-actions">
                        <span>{messages.confirmDeleteSavedReply(reply.title)}</span>
                        <button type="button" className="danger-action" disabled={busy} onClick={() => void run(() => onDelete(reply.id), () => setConfirming(null))}>{messages.confirmDelete}</button>
                        <button type="button" disabled={busy} onClick={() => setConfirming(null)}>{messages.cancel}</button>
                      </div>
                    : <div className="saved-reply-actions">
                        <button type="button" aria-label={messages.savedReplyHistoryOf(reply.title)} aria-expanded={panel === reply.id}
                          onClick={(event) => (panel === reply.id ? closePanel() : openPanel(reply.id, event.currentTarget))}>{messages.history}</button>
                        <button type="button" aria-label={messages.editSavedReplyName(reply.title)} onClick={() => { setProblem(undefined); setConfirming(null); setEditing({ id: reply.id, title: reply.title, body: reply.body }); }}>{messages.edit}</button>
                        <button type="button" className="danger-action" aria-label={messages.deleteSavedReplyName(reply.title)} onClick={() => { setProblem(undefined); setConfirming(reply.id); }}>{messages.confirmDelete}</button>
                      </div>}
                  {panel === reply.id && <Suspense fallback={<p className="muted-copy">{messages.loadingSavedReplyHistory}</p>}>
                    <SavedReplyHistory title={reply.title} timezone={timezone} load={() => loadHistory(reply.id)} onClose={closePanel}
                      onRestore={(version) => onRestoreVersion(reply.id, version, reply.updated_at)} /></Suspense>}
                </>}
          </li>
        ))}</ul>}
        <button type="button" className="saved-reply-trash-toggle" aria-expanded={panel === 'trash'}
          onClick={(event) => (panel === 'trash' ? closePanel() : openPanel('trash', event.currentTarget))}>{messages.savedReplyTrash}</button>
        {panel === 'trash' && <Suspense fallback={<p className="muted-copy">{messages.loadingSavedReplyHistory}</p>}>
          <SavedReplyTrash timezone={timezone} now={now ?? new Date()} load={loadTrash} onUndelete={onUndelete} onClose={closePanel} /></Suspense>}
      </div>
    </section>
  );
}
