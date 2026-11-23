import { useDeferredValue, useId, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { InvalidRequestError } from '../../api/client';
import type { Conversation, Message } from '../../api/schemas';
import { useCreateAction } from '../../api/use-create';
import { useHolds } from '../../lib/permissions-context';
import { Dialog, Field } from '../../ui/dialog';
import { HelpTip } from '../../ui/help-tip';
import { BookIcon, XIcon } from '../../ui/icons';
import { gapsApi } from '../kb/gaps-api';
import type { KBArticleRequest } from '../kb/gaps-api';
import { ActionMenu } from './action-menu';
import { articleRequest as m } from './article-request-messages';
import './article-request.css';

// Asking for a help center article from a conversation (docs/user/inbox.md "Request an article"): a button in
// the conversation header opens a dialog with the subject and the customer's last line filled in, so a request
// is two presses. A similar open request is offered first, so the conversation joins it instead of opening a
// second gap. And a menu on each sent reply keeps that reply as a draft article.

const maxLength = 300;
const firstLine = (text: string) => text.split('\n').map((line) => line.trim()).find(Boolean)?.slice(0, maxLength) ?? '';
const refusal = (error: unknown, fallback: string) => (error instanceof InvalidRequestError ? error.message : fallback);
const refreshGaps = (client: ReturnType<typeof useQueryClient>) => void client.invalidateQueries({ queryKey: ['kb-gaps'] });

interface Toast { text: string; undo: boolean }

export function RequestArticleButton({ conversation, timeline }: { conversation: Conversation; timeline: Message[] }) {
  const holds = useHolds();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [toast, setToast] = useState<Toast>();
  const [undoing, setUndoing] = useState(false);
  if (!holds('conversations:read') || !holds('kb_articles:read')) return null;
  const filed = (answer: KBArticleRequest) => {
    setOpen(false);
    refreshGaps(client);
    setToast(!answer.created ? { text: m.already(answer.gap.title), undo: false }
      : { text: answer.added_to_existing ? m.added(answer.gap.title) : m.requested, undo: true });
  };
  const undo = async () => {
    setUndoing(true);
    try {
      await gapsApi.withdraw(conversation.id);
      setToast({ text: m.undone, undo: false });
    } catch (error) {
      setToast({ text: refusal(error, m.undoFailed), undo: true });
    } finally {
      refreshGaps(client);
      setUndoing(false);
    }
  };
  return <>
    {/* On a phone the button is an icon in the title row, so the header keeps its two rows. */}
    <button type="button" className="article-request-button" title={m.buttonTip} onClick={() => setOpen(true)}>
      <BookIcon /><span className="article-request-button-label">{m.button}</span></button>
    {open && <RequestDialog conversation={conversation} timeline={timeline} onClose={() => setOpen(false)} onFiled={filed} />}
    {toast && <div role="status" className="article-request-toast"><span>{toast.text}</span>
      {toast.undo && <button type="button" className="link-action" disabled={undoing} onClick={() => void undo()}>{m.undo}</button>}
      <button type="button" className="icon-button" aria-label={m.dismiss} onClick={() => setToast(undefined)}><XIcon /></button></div>}
  </>;
}

interface DialogProps { conversation: Conversation; timeline: Message[]; onClose: () => void; onFiled: (answer: KBArticleRequest) => void }

function RequestDialog({ conversation, timeline, onClose, onFiled }: DialogProps) {
  const id = useId();
  const asked = [...timeline].reverse().find((item) => item.kind === 'incoming');
  const [title, setTitle] = useState(conversation.subject.slice(0, maxLength));
  const [note, setNote] = useState(() => firstLine(asked?.body ?? ''));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const words = useDeferredValue(title.trim());
  const similar = useQuery({
    queryKey: ['kb-gaps', 'similar', words], enabled: words !== '',
    queryFn: ({ signal }) => gapsApi.similar(words, signal),
  });
  const match = similar.data?.items[0];
  const submit = async () => {
    setBusy(true);
    setProblem(undefined);
    try {
      onFiled(await gapsApi.ask(conversation.id, {
        ...(title.trim() ? { title: title.trim() } : {}), ...(note.trim() ? { note: note.trim() } : {}), ...(match ? { gap_id: match.id } : {}),
      }));
    } catch (error) {
      setProblem(refusal(error, m.failed));
      setBusy(false);
    }
  };
  return <Dialog title={m.title} titleTip={<HelpTip label={m.aboutLabel} text={m.about} />} help={m.help} onClose={onClose} closeLabel={m.close} onSubmit={() => void submit()} footer={<>
    <button type="button" onClick={onClose}>{m.cancel}</button>
    <button type="submit" className="primary-action" disabled={busy}>{busy ? m.sending : match ? m.addToIt : m.request}</button>
  </>}>
    <Field id={`${id}-title`} label={m.question}>
      {(field) => <input {...field} autoFocus value={title} maxLength={maxLength} onChange={(event) => setTitle(event.target.value)} />}</Field>
    <Field id={`${id}-note`} label={m.note} help={m.noteHelp}>
      {(field) => <input {...field} value={note} maxLength={maxLength} onChange={(event) => setNote(event.target.value)} />}</Field>
    {match && <p className="article-request-similar">{m.similar(match.title, match.conversation_count)}</p>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </Dialog>;
}

// MakeArticleMenu is the menu on a sent reply that keeps it as a draft article, the customer's question beside
// it, and opens the draft in the editor.
export function MakeArticleMenu({ conversation, message }: { conversation: Conversation; message: Message }) {
  const holds = useHolds();
  const navigate = useNavigate();
  const make = useCreateAction((messageID: string, key: string) => gapsApi.fromReply(conversation.id, messageID, key));
  const [problem, setProblem] = useState<string>();
  if (!holds('kb_articles:create') || !holds('conversations:read')) return null;
  const run = () => {
    setProblem(undefined);
    make.run(message.id).then(
      (article) => navigate({ to: '/knowledge/articles/$articleId', params: { articleId: article.id } }),
      (error: unknown) => setProblem(refusal(error, m.makeFailed)),
    );
  };
  return <>
    <span className="make-article-menu">
      <ActionMenu<'make'> label={m.replyMenu} icon={null} disabled={make.pending} onApply={run}>
        {(apply) => <button type="button" onClick={() => apply('make')}>{m.makeArticle}</button>}
      </ActionMenu>
    </span>
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </>;
}
