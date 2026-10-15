import { useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import type { SavedReply } from '../../api/schemas';
import { messages } from '../../messages';

interface SavedRepliesSectionProps {
  replies: SavedReply[];
  // Each action resolves when the server accepted it and rejects with the reason when it did not.
  onCreate: (title: string, body: string) => Promise<unknown>;
  onUpdate: (id: string, change: { title: string; body: string }) => Promise<unknown>;
  onDelete: (id: string) => Promise<unknown>;
}

// Every agent manages the shared saved replies, so nothing here depends on the role.
export function SavedRepliesSection({ replies, onCreate, onUpdate, onDelete }: SavedRepliesSectionProps) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [editing, setEditing] = useState<{ id: string; title: string; body: string } | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  // run keeps the form open with the server's reason when an action is refused.
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
  return (
    <section className="settings-card" aria-labelledby="saved-replies-heading">
      <div className="section-heading"><h2 id="saved-replies-heading">{messages.savedReplies}</h2><p>{messages.savedRepliesDescription}</p></div>
      <div className="settings-body">
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {!editing && <form className="saved-reply-form" aria-label={messages.addSavedReply}
          onSubmit={(event) => { event.preventDefault(); if (!busy) void run(() => onCreate(title, body), () => { setTitle(''); setBody(''); }); }}>
          <label className="field"><span>{messages.savedReplyTitle}</span><input value={title} maxLength={200} onChange={(event) => setTitle(event.target.value)} /></label>
          <label className="field"><span>{messages.savedReplyText}</span><textarea rows={4} value={body} maxLength={20000} onChange={(event) => setBody(event.target.value)} /></label>
          <button className="primary-action" type="submit" disabled={busy || !title.trim() || !body.trim()}>{messages.addSavedReply}</button>
        </form>}
        {replies.length === 0 ? <p className="empty-copy">{messages.noSavedReplies}</p> : <ul className="saved-reply-list">{replies.map((reply) => (
          <li key={reply.id}>
            {editing?.id === reply.id
              ? <form className="saved-reply-form" aria-label={messages.editSavedReplyName(reply.title)}
                  onSubmit={(event) => { event.preventDefault(); if (!busy) void run(() => onUpdate(reply.id, { title: editing.title, body: editing.body }), () => setEditing(null)); }}>
                  <label className="field"><span>{messages.savedReplyTitle}</span><input value={editing.title} maxLength={200} onChange={(event) => setEditing({ ...editing, title: event.target.value })} /></label>
                  <label className="field"><span>{messages.savedReplyText}</span><textarea rows={4} value={editing.body} maxLength={20000} onChange={(event) => setEditing({ ...editing, body: event.target.value })} /></label>
                  <div className="saved-reply-actions">
                    <button className="primary-action" type="submit" disabled={busy || !editing.title.trim() || !editing.body.trim()}>{messages.saveSavedReply}</button>
                    <button type="button" disabled={busy} onClick={() => { setEditing(null); setProblem(undefined); }}>{messages.cancel}</button>
                  </div>
                </form>
              : <>
                  <div className="saved-reply-text"><strong>{reply.title}</strong><span>{reply.body}</span></div>
                  {confirming === reply.id
                    ? <div role="group" aria-label={messages.deleteSavedReplyName(reply.title)} className="saved-reply-actions">
                        <span>{messages.confirmDeleteSavedReply(reply.title)}</span>
                        <button type="button" className="danger-action" disabled={busy} onClick={() => void run(() => onDelete(reply.id), () => setConfirming(null))}>{messages.confirmDelete}</button>
                        <button type="button" disabled={busy} onClick={() => setConfirming(null)}>{messages.cancel}</button>
                      </div>
                    : <div className="saved-reply-actions">
                        <button type="button" aria-label={messages.editSavedReplyName(reply.title)} onClick={() => { setProblem(undefined); setConfirming(null); setEditing({ id: reply.id, title: reply.title, body: reply.body }); }}>{messages.edit}</button>
                        <button type="button" className="danger-action" aria-label={messages.deleteSavedReplyName(reply.title)} onClick={() => { setProblem(undefined); setConfirming(reply.id); }}>{messages.confirmDelete}</button>
                      </div>}
                </>}
          </li>
        ))}</ul>}
      </div>
    </section>
  );
}
