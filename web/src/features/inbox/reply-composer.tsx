import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { SavedReply } from '../../api/schemas';
import { renderSavedReply } from '../../lib/saved-reply-variables';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';
import { CcField } from './cc-field';
import type { AttachmentLimits } from './attachment-picker';
import { replyRefusalText } from './refusal-text';
import { track } from '../../lib/product-analytics';
import { SavedReplyPicker } from './saved-reply-picker';
import { articleLinkPart } from '../kb/kb-part';
import { messages } from '../../messages';
import { BookIcon, BookmarkIcon, LockIcon, ReplyIcon } from '../../ui/icons';
import { Part, PartUnavailable } from '../../ui/lazy-part';

interface ReplyComposerProps {
  disabled?: boolean;
  // May return a promise: the text is kept, with the reason shown, when it rejects.
  // Called with files only when some are chosen, and with cc (after the files, which may then be empty)
  // only for a reply that copies someone.
  onSubmit?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[]) => void | Promise<unknown>;
  // The addresses the customer copied on their latest message; the field starts with them until the agent edits it.
  suggestedCc?: string[];
  // The server's per-message file limits; the composer warns and holds the send while they are exceeded.
  attachmentLimits?: AttachmentLimits;
  // The workspace's saved replies; inserting one fills {{customer.name}} and {{agent.name}} from the two names.
  savedReplies?: SavedReply[];
  customerName?: string;
  agentName?: string;
  onManageSavedReplies?: () => void;
  savedRepliesState?: 'loading' | 'failed' | 'ready';
  // Opens the help center settings from the articles list while the help center is off; admins only.
  onManageHelpCenter?: () => void;
}

type Kind = 'outgoing' | 'note';
// The list open under the box: saved replies or the articles to link; one at a time.
type Picker = 'replies' | 'articles';
const shortcuts: Record<string, Kind> = { r: 'outgoing', n: 'note' };

const nonTextInputs = ['radio', 'checkbox', 'button', 'submit', 'reset'];

// Typing in a text field, or holding a modifier, is text entry and never a shortcut. Radios and
// checkboxes take no text, so R and N still work after a click on a status tab.
function isTyping(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target instanceof HTMLInputElement) return !nonTextInputs.includes(target.type);
  return target.isContentEditable || ['TEXTAREA', 'SELECT'].includes(target.tagName);
}

// An input method (Vietnamese Telex/VNI on macOS, CJK) still holds marked text while it composes;
// acting on that key would send or switch before the text is committed.
const composing = (event: KeyboardEvent) => event.isComposing || event.keyCode === 229;

const noAddresses: string[] = [];

export function ReplyComposer({ disabled, onSubmit, attachmentLimits, savedReplies = [], customerName, agentName, onManageSavedReplies, savedRepliesState, onManageHelpCenter, suggestedCc = noAddresses }: ReplyComposerProps) {
  const [kind, setKind] = useState<Kind>('outgoing');
  const [body, setBody] = useState('');
  const [problem, setProblem] = useState<string>();
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const fileProblem = attachmentProblem(files, attachmentLimits);
  // undefined until the agent edits the list, so suggestions that load later still show up.
  const [ccEdited, setCcEdited] = useState<string[]>();
  const [ccOpen, setCcOpen] = useState(false);
  const [ccBlocking, setCcBlocking] = useState(false);
  const cc = ccEdited ?? suggestedCc;
  const showCc = kind === 'outgoing' && (ccOpen || ccEdited !== undefined || cc.length > 0);
  const card = useRef<HTMLDivElement>(null);
  const focusCc = useRef(false);
  useEffect(() => {
    if (!focusCc.current || !showCc) return;
    focusCc.current = false;
    card.current?.querySelector<HTMLInputElement>('.composer-cc input')?.focus();
  }, [showCc]);
  const field = useRef<HTMLTextAreaElement>(null);
  const [picking, setPicking] = useState<Picker | null>(null);
  const toggle = (picker: Picker) => setPicking((open) => (open === picker ? null : picker));
  // Where the cursor goes once an inserted reply is in the box.
  const caret = useRef<number | null>(null);
  // Inserting adds text at the cursor and replaces nothing; the rest of the draft and the files stay.
  const insertText = (text: string) => {
    const box = field.current;
    const at = Math.min(box?.selectionStart ?? body.length, body.length);
    setPicking(null);
    // The browser inserts the text as if it were typed, so Ctrl+Z takes it out again. It fires the
    // input event that updates the draft. Without it (or when it refuses) the composer writes the text.
    if (box) {
      box.focus();
      box.setSelectionRange(at, at);
      if (typeof document.execCommand === 'function' && document.execCommand('insertText', false, text)) return;
    }
    caret.current = at + text.length;
    setBody(body.slice(0, at) + text + body.slice(at));
  };
  const insertSavedReply = (reply: SavedReply) => {
    track('saved_reply_inserted');
    insertText(renderSavedReply(reply.body, { customerName, agentName }));
  };
  // Mail goes out as plain text, so a link is the title and the address on two lines, not Markdown. Both
  // get lines of their own: text right after the address would become part of it in the customer's mail.
  const insertArticle = (article: { title: string; url: string }) => {
    const at = Math.min(field.current?.selectionStart ?? body.length, body.length);
    const before = at > 0 && body[at - 1] !== '\n' ? '\n' : '';
    const after = at < body.length && body[at] !== '\n' ? '\n' : '';
    insertText(`${before}${article.title}\n${article.url}${after}`);
  };
  const closePicker = (restoreFocus: boolean) => {
    setPicking(null);
    if (restoreFocus) field.current?.focus();
  };
  useEffect(() => {
    if (caret.current === null || picking) return;
    field.current?.focus();
    field.current?.setSelectionRange(caret.current, caret.current);
    caret.current = null;
  }, [body, picking]);
  useEffect(() => { if (disabled) setPicking(null); }, [disabled]);
  // The text leaves the box only after the server accepted it; a refusal keeps it with the reason.
  // An address typed into Cc that cannot be added would otherwise be dropped from the reply unseen.
  const ccHolds = kind === 'outgoing' && showCc && ccBlocking;
  const submit = () => {
    if (!body.trim() || sending || fileProblem || ccHolds) return;
    // A variable no saved reply filled in would reach the customer as written.
    const unfilled = kind === 'outgoing' ? [...new Set(body.match(/\{\{[^{}]*\}\}/g) ?? [])] : [];
    if (unfilled.length > 0 && !window.confirm(messages.unfilledVariables(unfilled))) return;
    const sent = body.trim();
    const sentFiles = files;
    const sentCc = kind === 'outgoing' ? cc : [];
    const result = sentCc.length > 0 ? onSubmit?.(kind, sent, sentFiles, sentCc)
      : sentFiles.length > 0 ? onSubmit?.(kind, sent, sentFiles) : onSubmit?.(kind, sent);
    if (typeof (result as Promise<unknown> | undefined)?.then !== 'function') { setBody(''); setFiles([]); return; }
    setSending(true);
    setProblem(undefined);
    // Clear only what was sent: text typed while the request was in flight (a note right after a reply) stays.
    // The files go with it; files picked meanwhile stay.
    const clearSent = () => {
      setBody((current) => (current.trim() === sent ? '' : current));
      setFiles((current) => current.filter((file) => !sentFiles.includes(file)));
    };
    (result as Promise<unknown>).then(clearSent, (error: unknown) => setProblem(replyRefusalText(error))).finally(() => setSending(false));
  };
  // R starts a reply and N a private note from anywhere on the page, as the keycaps on the tabs say.
  useEffect(() => {
    if (disabled) return;
    const onKey = (event: KeyboardEvent) => {
      if (composing(event) || event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return;
      if (event.key === '/') { event.preventDefault(); setPicking('replies'); return; }
      const next = shortcuts[event.key.toLowerCase()];
      if (!next) return;
      event.preventDefault();
      setKind(next);
      field.current?.focus();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [disabled]);
  const onFieldKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (composing(event.nativeEvent)) return;
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); submit(); }
    if (event.key === '/' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); setPicking('replies'); }
  };
  return (
    <div className="composer">
      <div className="composer-card" data-kind={kind} ref={card}>
        <div className="composer-head">
          <div className="composer-tabs" role="group" aria-label={messages.messageKind}>
            <button type="button" aria-pressed={kind === 'outgoing'} onClick={() => setKind('outgoing')}>
              <ReplyIcon />{messages.replyMode}<kbd aria-hidden="true">R</kbd></button>
            <button type="button" aria-pressed={kind === 'note'} onClick={() => setKind('note')}>
              <LockIcon />{messages.noteMode}<kbd aria-hidden="true">N</kbd></button>
          </div>
          {kind === 'note' && <span className="composer-hint">{messages.noteHint}</span>}
        </div>
        <textarea ref={field} aria-label={messages.messageBody} value={body} disabled={disabled} onKeyDown={onFieldKey}
          placeholder={kind === 'note' ? messages.notePlaceholder : messages.replyPlaceholder} onChange={(event) => setBody(event.target.value)} />
        {picking === 'replies' && !disabled && <SavedReplyPicker replies={savedReplies} onPick={insertSavedReply} onClose={closePicker} onManage={onManageSavedReplies} state={savedRepliesState} />}
        {picking === 'articles' && !disabled && <Part part={articleLinkPart} failed={<PartUnavailable />}
          render={({ ArticleLinkPicker }) => <ArticleLinkPicker onPick={insertArticle} onClose={closePicker} onManageHelpCenter={onManageHelpCenter} />} />}
        {problem && <p role="alert" className="composer-notice">{problem}</p>}
        {showCc && <CcField value={cc} onChange={setCcEdited} disabled={disabled || sending} onSend={submit} onBlockingChange={setCcBlocking} />}
        <AttachmentPicker files={files} onChange={setFiles} limits={attachmentLimits} disabled={disabled || sending} />
        <div className="composer-actions">
          {/* Mouse down must not blur the open list, or the click would close it and open it again. */}
          <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'replies'} title={`${messages.insertSavedReply} (/)`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('replies')}>
            <BookmarkIcon />{messages.insertSavedReply}<kbd aria-hidden="true">/</kbd></button>
          {/* The articles list is in the knowledge base chunk; pointing at the button starts loading it. */}
          <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'articles'} title={messages.linkArticle}
            onPointerEnter={articleLinkPart.preload} onFocus={articleLinkPart.preload} onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('articles')}>
            <BookIcon />{messages.articles}</button>
          {kind === 'outgoing' && !showCc && (
            <button type="button" className="cc-button" disabled={disabled} aria-expanded={false} title={messages.ccToggleTitle}
              onClick={() => { focusCc.current = true; setCcOpen(true); }}>{messages.ccToggle}</button>
          )}
          <span className="composer-hint" aria-hidden="true"><kbd>Ctrl</kbd><kbd>Enter</kbd> {messages.sendShortcut}</span>
          <button className="primary-action" type="button" disabled={disabled || sending || !body.trim() || Boolean(fileProblem) || ccHolds} onClick={submit}>
            {kind === 'note' ? messages.privateNote : messages.reply}
          </button>
        </div>
      </div>
    </div>
  );
}
