import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { Message, SavedReply } from '../../api/schemas';
import { renderSavedReply } from '../../lib/saved-reply-variables';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';
import { CcField } from './cc-field';
import type { AttachmentLimits } from './attachment-picker';
import { replyRefusalText } from './refusal-text';
import { draftPrefix, parseDraft, readDraft, writeDraft } from './reply-draft-storage';
import type { StoredDraft } from './reply-draft-storage';
import { ConflictError, ReplyConflictError } from '../../api/client';
import { presenceViewPart } from './presence-part';
import { track } from '../../lib/product-analytics';
import { composing, isTyping } from '../../lib/typing-target';
import { messages } from '../../messages';
import { BookIcon, BookmarkIcon, LockIcon, ReplyIcon, SparklesIcon } from '../../ui/icons';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';

interface ReplyComposerProps {
  disabled?: boolean;
  // noReplies: no channel carries replies of this conversation (it was removed); private notes still go.
  noReplies?: boolean;
  // May return a promise: the text is kept, with the reason shown, when it rejects.
  // Called with files only when some are chosen, and with cc (after the files, which may then be empty)
  // only for a reply that copies someone. aiDraft (last) names the AI draft a reply was written from.
  onSubmit?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[], seen?: number, aiDraft?: string) => void | Promise<unknown>;
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
  // An admin, who can turn the help center on from the articles list while it is off.
  canManage?: boolean;
  // Whether the box holds a reply to the customer not sent yet (never a private note), each time that
  // changes, and each change of that reply's text: the team sees that someone is writing, never the text.
  onReplyDraft?: (writing: boolean) => void;
  onReplyEdited?: () => void;
  // The newest reply number on screen. With it, a reply carries the number shown when its writer started
  // (passed to onSubmit after cc), and a refusal for a newer reply (ReplyConflictError) shows that reply
  // with Send anyway and Edit reply. timezone formats its time.
  latestReplyNumber?: number;
  timezone?: string;
  // Reply drafts are on: the box offers Draft with AI for this conversation. agentId finds the person's own drafts.
  aiDraftsFor?: string;
  // The id of the customer's newest message: when it changes, the automatic draft is read again.
  latestCustomerMessage?: string;
  // Reply drafts are off: the box says so, with the way to turn them on (canManage) or whom to ask.
  aiDraftsOff?: boolean;
  agentId?: string;
  // Sets the conversation Resolved. With it a reply offers Send and resolve (Ctrl/Cmd+Shift+Enter), which
  // calls it only after the reply was accepted. A private note never resolves.
  onResolve?: () => Promise<unknown>;
  // Where this browser keeps the unsent text (workspace, agent and conversation); without it nothing is kept.
  draftKey?: string;
  // The conversation's messages once loaded (undefined while loading). A draft kept after a send that was
  // cut off (a reload) is dropped when its message is there: the server stored it after all.
  timeline?: Message[];
}

type Kind = 'outgoing' | 'note';
// The list open under the box: saved replies or the articles to link; one at a time.
type Picker = 'replies' | 'articles';
const shortcuts: Record<string, Kind> = { r: 'outgoing', n: 'note' };


const noAddresses: string[] = [];

// The lists under the box are a chunk of their own, so the first bundle carries neither. The composer
// starts loading it when it mounts, which is when a conversation opens: a click a moment later finds it.
export const replyPickersPart = lazyPart(() => import('./reply-pickers'));
// The AI draft panel loads the same way, only where reply drafts are on.
export const aiDraftPart = lazyPart(() => import('./ai-draft-part'));

// A reply named a draft that was discarded, expired or replaced meanwhile: it goes without the draft.
const draftGone = (error: unknown) => error instanceof ConflictError && error.detail?.code === 'ai_draft_not_ready';

export function ReplyComposer({ disabled, noReplies = false, onSubmit, attachmentLimits, savedReplies = [], customerName, agentName, onManageSavedReplies, savedRepliesState, canManage, suggestedCc = noAddresses, onReplyDraft, onReplyEdited, latestReplyNumber, latestCustomerMessage, timezone = 'UTC', aiDraftsFor, aiDraftsOff = false, agentId, onResolve, draftKey, timeline }: ReplyComposerProps) {
  // With no channel to carry replies the box opens on a private note, the one thing that can be sent.
  const [stored] = useState(() => readDraft(draftKey));
  const [kind, setKind] = useState<Kind>(noReplies ? 'note' : stored?.kind ?? 'outgoing');
  const [body, setBody] = useState(stored?.body ?? '');
  // The text is kept as it is typed, and forgotten once the box is empty again (after a send). The key is
  // read once: a page whose key arrives later (the workspace loads after the conversation on a reload)
  // remounts the box with it, so the draft kept there is read before anything is written over it.
  useEffect(() => writeDraft(draftKey, { kind, body }), [draftKey, kind, body]);
  const latestBody = useRef(body);
  latestBody.current = body;
  const latestKind = useRef(kind);
  latestKind.current = kind;
  // A draft written back after its send was cut off: once the timeline shows that message arrived, the
  // text leaves the box instead of inviting a second send.
  const cutOff = useRef(stored?.sentAt === undefined ? undefined : stored);
  useEffect(() => {
    const sent = cutOff.current;
    if (!sent || !timeline) return;
    cutOff.current = undefined;
    // A few minutes either way: the browser's clock and the server's may differ.
    const arrived = timeline.some((item) => item.kind === sent.kind && item.body.trim() === sent.body.trim()
      && (!agentId || item.author_agent_id === agentId) && Date.parse(item.created_at) >= (sent.sentAt ?? 0) - 5 * 60_000);
    if (arrived && latestBody.current.trim() === sent.body.trim()) setBody('');
  }, [timeline, agentId]);
  // Another tab of this browser wrote (or sent) this draft: a box not edited here shows its text, as a reply
  // or a note the way that tab had it (a note must never turn into a reply), and says so. Text typed here is
  // never replaced unasked: the person picks theirs or the other tab's.
  const [otherTab, setOtherTab] = useState(false);
  const editedHere = useRef(false);
  const [otherDraft, setOtherDraft] = useState<StoredDraft>();
  const adopt = (draft: StoredDraft) => {
    setKind(draft.kind);
    setBody(draft.body);
    editedHere.current = false;
    setOtherDraft(undefined);
  };
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (!draftKey || event.key !== draftPrefix + draftKey) return;
      const kept = readDraft(draftKey);
      const text = kept?.body ?? '';
      const nextKind = noReplies ? 'note' : kept?.kind ?? latestKind.current;
      if (text === latestBody.current && nextKind === latestKind.current) { setOtherDraft(undefined); return; }
      // The other tab sent or cleared the very text this box holds: it goes here too.
      const sameTextGone = !text.trim() && parseDraft(event.oldValue)?.body === latestBody.current;
      if (editedHere.current && latestBody.current.trim() && !sameTextGone) {
        setOtherTab(false);
        setOtherDraft({ kind: nextKind, body: text });
        return;
      }
      adopt({ kind: nextKind, body: text });
      setOtherTab(true);
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [draftKey, noReplies]);
  const [problem, setProblem] = useState<string>();
  const [sending, setSending] = useState(false);
  // Set from the press until the answer: clicks that land before the button turns disabled send nothing.
  const inFlight = useRef(false);
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
  const pickers = replyPickersPart.use();
  const [assist, setAssist] = useState(false);
  // A draft is being written in the open panel: the button says so and keeps the panel (and its Stop) open.
  const [drafting, setDrafting] = useState(false);
  const [offNote, setOffNote] = useState(false);
  // The AI draft the reply was written from, until it is sent or the box is emptied.
  const [aiDraft, setAIDraft] = useState<string>();
  // What the draft put in the box that the conversation does not hold: shown until it is sent or the box is emptied.
  const [aiWarnings, setAIWarnings] = useState<string[]>([]);
  const assistPart = aiDraftPart.use(Boolean(aiDraftsFor));
  useEffect(() => { if (!body.trim()) { setAIDraft(undefined); setAIWarnings([]); } }, [body]);
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
  useEffect(() => { if (disabled) { setPicking(null); setAssist(false); } }, [disabled]);
  const applyDraft = (text: string, how: 'replace' | 'below', id: string, warnings: string[] = []) => {
    setAssist(false);
    setKind('outgoing');
    setAIDraft(id);
    setAIWarnings(warnings);
    const next = how === 'replace' || !body.trim() ? text : `${body.trimEnd()}\n\n${text}`;
    caret.current = next.length;
    setBody(next);
  };
  const reply = kind === 'outgoing' ? body.trim() : '';
  const writing = reply !== '';
  useEffect(() => { onReplyDraft?.(writing); }, [writing, onReplyDraft]);
  // The newest reply on screen when the reply was started: a teammate's reply arriving later, even on
  // screen, is asked about once when this one is sent.
  const latest = useRef(latestReplyNumber);
  const mark = useRef<number | undefined>(undefined);
  useEffect(() => { latest.current = latestReplyNumber; }, [latestReplyNumber]);
  useEffect(() => { mark.current = writing ? mark.current ?? latest.current : undefined; }, [writing]);
  const [conflict, setConflict] = useState<ReplyConflictError>();
  useEffect(() => { if (reply) onReplyEdited?.(); }, [reply, onReplyEdited]);
  // The text leaves the box only after the server accepted it; a refusal keeps it with the reason.
  // An address typed into Cc that cannot be added would otherwise be dropped from the reply unseen.
  const ccHolds = kind === 'outgoing' && showCc && ccBlocking;
  const canResolve = kind === 'outgoing' && Boolean(onResolve);
  // Whether the last reply asked to resolve too, so Send anyway after a conflict keeps that choice.
  const resolveAfter = useRef(false);
  const blocked = kind === 'outgoing' && noReplies;
  const cannotSend = sending || !body.trim() || Boolean(fileProblem) || ccHolds || blocked;
  const submit = (resolve = false) => {
    if (!body.trim() || inFlight.current || fileProblem || ccHolds || blocked) return;
    const resolving = resolve && canResolve;
    resolveAfter.current = resolving;
    // The reply stays sent when the status change fails; the person is told and can press Resolve. The button
    // goes once the conversation is resolved, so focus that went with it comes back to the box.
    const resolveNow = () => (resolving ? onResolve!().then(() => {
      if (!document.activeElement || document.activeElement === document.body) field.current?.focus();
    }, () => setProblem(messages.resolvedAfterReplyFailed)) : undefined);
    // A variable no saved reply filled in would reach the customer as written.
    const unfilled = kind === 'outgoing' ? [...new Set(body.match(/\{\{[^{}]*\}\}/g) ?? [])] : [];
    if (unfilled.length > 0 && !window.confirm(messages.unfilledVariables(unfilled))) return;
    const sent = body.trim();
    const sentFiles = files;
    const sentCc = kind === 'outgoing' ? cc : [];
    const seen = kind === 'outgoing' && latestReplyNumber !== undefined ? mark.current ?? latestReplyNumber : undefined;
    const fromDraft = kind === 'outgoing' ? aiDraft : undefined;
    setConflict(undefined);
    // The kept draft goes before the request leaves, so a reload meanwhile does not offer the sent text
    // again; a failure writes it back, marked as sent once.
    const sentAt = Date.now();
    writeDraft(draftKey, { kind, body: '' });
    const send = (draftId?: string) => draftId ? onSubmit?.(kind, sent, sentFiles, sentCc, seen, draftId)
      : seen !== undefined ? onSubmit?.(kind, sent, sentFiles, sentCc, seen)
      : sentCc.length > 0 ? onSubmit?.(kind, sent, sentFiles, sentCc)
      : sentFiles.length > 0 ? onSubmit?.(kind, sent, sentFiles) : onSubmit?.(kind, sent);
    let result = send(fromDraft);
    if (fromDraft && typeof (result as Promise<unknown> | undefined)?.then === 'function') {
      result = (result as Promise<unknown>).catch((error: unknown) => (draftGone(error) ? send() : Promise.reject(error)));
    }
    if (typeof (result as Promise<unknown> | undefined)?.then !== 'function') { setBody(''); setFiles([]); void resolveNow(); return; }
    inFlight.current = true;
    setSending(true);
    setProblem(undefined);
    // Clear only what was sent: text typed while the request was in flight (a note right after a reply) stays.
    // The files go with it; files picked meanwhile stay.
    const clearSent = () => {
      if (fromDraft) { setAIDraft(undefined); setAIWarnings([]); }
      setBody((current) => (current.trim() === sent ? '' : current));
      setFiles((current) => current.filter((file) => !sentFiles.includes(file)));
    };
    (result as Promise<unknown>).then(() => { clearSent(); return resolveNow(); }, (error: unknown) => {
      writeDraft(draftKey, { kind, body: latestBody.current, sentAt });
      if (error instanceof ReplyConflictError) setConflict(error);
      else setProblem(replyRefusalText(error));
    }).finally(() => { inFlight.current = false; setSending(false); });
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
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); submit(event.shiftKey); }
    if (event.key === '/' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); setPicking('replies'); }
  };
  return (
    <div className="composer">
      {aiDraftsFor && !disabled && assistPart.status === 'ready' && <assistPart.module.AutoDraftCard conversationId={aiDraftsFor} customerName={customerName}
        timezone={timezone} hasText={Boolean(body.trim())} canManage={canManage} latestCustomerMessage={latestCustomerMessage} onUse={applyDraft}
        onTryAgain={() => setAssist(true)} />}
      <div className="composer-card" data-kind={kind} ref={card}>
        <div className="composer-head">
          <div className="composer-tabs" role="group" aria-label={messages.messageKind}>
            <button type="button" aria-pressed={kind === 'outgoing'} onClick={() => setKind('outgoing')}>
              <ReplyIcon />{messages.replyMode}<kbd aria-hidden="true">R</kbd></button>
            <button type="button" aria-pressed={kind === 'note'} onClick={() => setKind('note')}>
              <LockIcon />{messages.noteMode}<kbd aria-hidden="true">N</kbd></button>
          </div>
          <div className="composer-head-end">
            {kind === 'note' && <span className="composer-hint">{messages.noteHint}</span>}
            {/* The keys sit up here, where there is room, so the buttons below keep one row. */}
            <span className="composer-hint composer-keys" aria-hidden="true"><kbd>Ctrl</kbd><kbd>Enter</kbd> {messages.sendShortcut}
              {canResolve && <span className="composer-keys-resolve">{messages.resolveShortcutAdd} <kbd>Shift</kbd> {messages.resolveShortcut}</span>}</span>
          </div>
        </div>
        {aiWarnings.length > 0 && kind === 'outgoing' && (
          <p className="composer-warning" role="alert"><strong>{messages.checkBeforeSending}</strong> {aiWarnings.join(', ')}</p>
        )}
        <textarea ref={field} aria-label={messages.messageBody} value={body} disabled={disabled} onKeyDown={onFieldKey}
          placeholder={kind === 'note' ? messages.notePlaceholder : noReplies ? messages.replyNotSendable : messages.replyPlaceholder} onChange={(event) => { setBody(event.target.value); setOtherTab(false); editedHere.current = true; }} />
        {otherTab && <p role="status" className="composer-hint">{messages.draftFromOtherTab}</p>}
        {otherDraft && <p role="status" className="composer-hint">{messages.otherTabDraftDiffers}{' '}
          <button type="button" className="link-action" onClick={() => adopt(otherDraft)}>{messages.useOtherTabDraft}</button>{' '}
          <button type="button" className="link-action" onClick={() => { writeDraft(draftKey, { kind, body }); setOtherDraft(undefined); }}>{messages.keepMyDraft}</button></p>}
        {picking && !disabled && (pickers.status === 'error' ? <div className="saved-reply-picker"><PartUnavailable /></div>
          // Until the chunk is there, the list's own box says it is loading, where the list will be.
          : pickers.status === 'loading' ? <div className="saved-reply-picker"><p className="saved-reply-empty" role="status">{messages.loadingList}</p></div>
          : picking === 'replies' ? <pickers.module.SavedReplyPicker replies={savedReplies} onPick={insertSavedReply} onClose={closePicker} onManage={onManageSavedReplies} state={savedRepliesState} />
          : <pickers.module.ArticleLinkPicker box={field} onInsert={insertText} onClose={closePicker} canManage={canManage} />)}
        {assist && aiDraftsFor && !disabled && (assistPart.status === 'error' ? <div className="saved-reply-picker"><PartUnavailable /></div>
          : assistPart.status === 'loading' ? <div className="saved-reply-picker"><p className="saved-reply-empty" role="status">{messages.loadingList}</p></div>
          : <assistPart.module.AIDraftPanel conversationId={aiDraftsFor} agentId={agentId} hasText={Boolean(body.trim())} onUse={applyDraft} canManage={canManage} onWriting={setDrafting}
            onClose={() => { setAssist(false); field.current?.focus(); }} />)}
        {conflict && <Part part={presenceViewPart} failed={<p role="alert" className="composer-notice">{conflict.message}</p>}
          render={(view) => <view.ReplyConflict replies={conflict.newer} timezone={timezone}
            onSendAnyway={() => { mark.current = conflict.latest; submit(resolveAfter.current); }}
            onEdit={() => { mark.current = conflict.latest; setConflict(undefined); field.current?.focus(); }} />} />}
        {problem && <p role="alert" className="composer-notice">{problem}</p>}
        {offNote && !aiDraftsFor && <p role="status" className="composer-notice">{messages.aiDraftsOffAgent}</p>}
        {showCc && <CcField value={cc} onChange={setCcEdited} disabled={disabled || sending} onSend={submit} onBlockingChange={setCcBlocking} />}
        <AttachmentPicker files={files} onChange={setFiles} limits={attachmentLimits} disabled={disabled || sending} />
        <div className="composer-actions">
          {/* Mouse down must not blur the open list, or the click would close it and open it again. */}
          <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'replies'} title={`${messages.insertSavedReply} (/)`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('replies')}>
            <BookmarkIcon /><span className="tool-label">{messages.insertSavedReply}</span><kbd aria-hidden="true">/</kbd></button>
          <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'articles'}
            onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('articles')}>
            <BookIcon /><span className="tool-label">{messages.articles}</span></button>
          {aiDraftsFor && (
            <button type="button" className="saved-reply-button ai-tool" disabled={disabled} aria-expanded={assist} aria-disabled={assist && drafting ? true : undefined}
              onClick={() => { if (!(assist && drafting)) setAssist((open) => !open); }}>
              <SparklesIcon /><span className="tool-label">{assist && drafting ? messages.drafting : messages.draftWithAI}</span>
              {assistPart.status === 'ready' && !assist && !aiDraft && <assistPart.module.DraftStatus conversationId={aiDraftsFor} agentId={agentId} />}</button>
          )}
          {!aiDraftsFor && aiDraftsOff && (canManage
            // Its name says what it is (the app's own Settings link keeps the name "Settings"); how to turn
            // drafts on is its description.
            ? <a className="saved-reply-button" href="/settings/ai" target="_blank" rel="noopener" aria-label={messages.aiDraftsOffName} title={messages.aiDraftsOffAdmin}>
                <SparklesIcon /><span className="tool-label">{messages.draftWithAI}</span><span className="ai-off-label">{messages.aiDraftsOff}</span></a>
            : <button type="button" className="saved-reply-button" aria-disabled="true" title={messages.aiDraftsOffAgent} onClick={() => setOffNote((shown) => !shown)}>
                <SparklesIcon /><span className="tool-label">{messages.draftWithAI}</span><span className="ai-off-label">{messages.aiDraftsOff}</span></button>)}
          {kind === 'outgoing' && !showCc && (
            <button type="button" className="cc-button" disabled={disabled} aria-expanded={false} title={messages.ccToggleTitle}
              onClick={() => { focusCc.current = true; setCcOpen(true); }}>{messages.ccToggle}</button>
          )}
          {canResolve && <button type="button" className="send-and-resolve" disabled={disabled || cannotSend}
            aria-keyshortcuts="Control+Shift+Enter Meta+Shift+Enter" title={`${messages.sendAndResolve} (Ctrl+Shift+Enter)`} onClick={() => submit(true)}>
            {messages.sendAndResolve}</button>}
          <button className="primary-action" type="button" disabled={disabled || cannotSend} onClick={() => submit()}>
            {kind === 'note' ? messages.privateNote : messages.reply}
          </button>
        </div>
      </div>
    </div>
  );
}
