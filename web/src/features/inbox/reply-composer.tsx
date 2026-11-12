import { useCallback, useEffect, useId, useRef, useState } from 'react';
import type { FocusEvent, KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { SavedReply } from '../../api/schemas';
import { newSubmitKey } from '../../api/submit-key';
import { renderSavedReply } from '../../lib/saved-reply-variables';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';
import { useHolds } from '../../lib/permissions-context';
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
import { ArrowUpIcon, BookIcon, BookmarkIcon, LockIcon, ReplyIcon, SparklesIcon } from '../../ui/icons';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';
import { matchingTeammates, mentionAt, mentionedIn, MentionPicker, optionIdOf } from './mention-picker';
import type { Teammate } from './mention-picker';
import { mentionMessages } from './mention-messages';

interface ReplyComposerProps {
  disabled?: boolean;
  // noReplies: no channel carries replies of this conversation (it was removed); private notes still go.
  noReplies?: boolean;
  // May return a promise: the text is kept, with the reason shown, when it rejects.
  // Called with files only when some are chosen, and with cc (after the files, which may then be empty)
  // only for a reply that copies someone. aiDraft (last) names the AI draft a reply was written from.
  // With draftKey, every argument is passed and key (last) is the send's Idempotency-Key, kept with the draft.
  // A note that asks teammates passes their ids last (mentions), after key.
  onSubmit?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[], seen?: number, aiDraft?: string, key?: string, mentions?: string[]) => void | Promise<unknown>;
  // The teammates a private note can ask by typing @ (askableTeammates); without any, @ is plain text.
  teammates?: Teammate[];
  // findSent says whether the send under key stored its message; a draft kept with a send's key asks it once
  // when the box opens, and is dropped when it did. Without it such a draft is offered again.
  findSent?: (key: string) => Promise<boolean>;
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
  // Reply drafts are off: an admin (canManage) sees the tool as Off, with the way to turn them on.
  aiDraftsOff?: boolean;
  agentId?: string;
  // Sets the conversation Resolved. With it a reply offers Send and resolve (Ctrl/Cmd+Shift+Enter), which
  // calls it only after the reply was accepted. A private note never resolves.
  onResolve?: () => Promise<unknown>;
  // The chat channel that tells the customer when it is resolved, said in Send and resolve's tooltip.
  resolveTells?: string;
  // Where this browser keeps the unsent text (workspace, agent and conversation); without it nothing is kept.
  draftKey?: string;
}

type Kind = 'outgoing' | 'note';
// The list open under the box: saved replies or the articles to link; one at a time.
type Picker = 'replies' | 'articles';
const shortcuts: Record<string, Kind> = { r: 'outgoing', n: 'note' };


const noAddresses: string[] = [];
const noTeammates: Teammate[] = [];

// inputOf names the files and copies of a send: each list counted first, a file by name, size and date (its
// bytes are not read), so a changed attachment or Cc after a lost answer is a new message, never a replay.
const inputOf = (files: File[], cc: string[]) =>
  JSON.stringify([cc.length, ...cc, files.length, ...files.map((file) => [file.name, file.size, file.lastModified])]);
const noInput = inputOf([], []);

// PendingSend is a send in flight, failed or cut off: its text, files and copies, and its Idempotency-Key.
interface PendingSend { kind: Kind; body: string; key: string; input: string }

// The lists under the box are a chunk of their own, so the first bundle carries neither. The composer
// starts loading it when it mounts, which is when a conversation opens: a click a moment later finds it.
export const replyPickersPart = lazyPart(() => import('./reply-pickers'));
// The AI draft panel loads the same way, only where reply drafts are on.
export const aiDraftPart = lazyPart(() => import('./ai-draft-part'));

// A reply named a draft that was discarded, expired or replaced meanwhile: it goes without the draft.
const draftGone = (error: unknown) => error instanceof ConflictError && error.detail?.code === 'ai_draft_not_ready';

export function ReplyComposer({ disabled, noReplies = false, onSubmit, attachmentLimits, savedReplies = [], customerName, agentName, onManageSavedReplies, savedRepliesState, canManage, suggestedCc = noAddresses, onReplyDraft, onReplyEdited, latestReplyNumber, latestCustomerMessage, timezone = 'UTC', aiDraftsFor, aiDraftsOff = false, agentId, onResolve, resolveTells, draftKey, findSent, teammates = noTeammates }: ReplyComposerProps) {
  // What the role holds: a reply needs messages:send, the lists under the box their own read.
  const holds = useHolds();
  const mayReply = holds('messages:send');
  const maySavedReplies = holds('saved_replies:read');
  const mayArticles = holds('kb_articles:read');
  // With no channel to carry replies, or a role that may not write to customers, the box opens on a private
  // note, the one thing that can be sent.
  const [stored] = useState(() => readDraft(draftKey));
  const [kind, setKindAsked] = useState<Kind>(noReplies || !mayReply ? 'note' : stored?.kind ?? 'outgoing');
  const setKind = useCallback((next: Kind) => setKindAsked(next === 'outgoing' && !mayReply ? 'note' : next), [mayReply]);
  const [body, setBody] = useState(stored?.body ?? '');
  // The text is kept as it is typed, and forgotten once the box is empty again (after a send). The key is
  // read once: a page whose key arrives later (the workspace loads after the conversation on a reload)
  // remounts the box with it, so the draft kept there is read before anything is written over it.
  // A send in flight, failed or cut off keeps its key with the text while the text and kind are unchanged:
  // sending it again goes under the same key, so the server stores it once.
  // The files and copies of a send are part of it too (input, from inputOf): other ones are a new message.
  // Another tab of this browser sending the same text wrote its key with the draft: that key is this one's.
  const pendingSend = useRef<PendingSend | undefined>(stored?.sendKey ? { kind: stored.kind, body: stored.body, key: stored.sendKey, input: stored.sendInput ?? noInput } : undefined);
  const sendOf = (kind: Kind, body: string, input?: string): PendingSend | undefined => {
    const kept = readDraft(draftKey);
    const fromStorage = kept?.sendKey ? { kind: kept.kind, body: kept.body, key: kept.sendKey, input: kept.sendInput ?? noInput } : undefined;
    return [pendingSend.current, fromStorage].find((send) => send && send.kind === kind && send.body === body && (input === undefined || send.input === input));
  };
  useEffect(() => {
    const send = sendOf(kind, body);
    writeDraft(draftKey, { kind, body, ...(send ? { sendKey: send.key, sendInput: send.input } : {}) });
  }, [draftKey, kind, body]);
  const latestBody = useRef(body);
  latestBody.current = body;
  const latestKind = useRef(kind);
  latestKind.current = kind;
  // A draft kept with a send's key asks the server once whether that send stored its message: then the text
  // leaves the box instead of inviting a second send. It is never sent by itself.
  useEffect(() => {
    const send = pendingSend.current;
    if (!send || !findSent) return;
    let current = true;
    findSent(send.key).then((found) => {
      if (!current || !found || pendingSend.current !== send) return;
      pendingSend.current = undefined;
      if (latestBody.current === send.body) setBody('');
      else writeDraft(draftKey, { kind: latestKind.current, body: latestBody.current });
    }, () => undefined);
    return () => { current = false; };
  }, []);
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
      const mine = editedHere.current && latestBody.current.trim() !== '';
      // It sent or cleared text of its own: the text typed here stays, and is kept again.
      if (mine && !text.trim() && !sameTextGone) {
        writeDraft(draftKey, { kind: latestKind.current, body: latestBody.current });
        return;
      }
      if (mine && !sameTextGone) {
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
  // The @ being typed in a private note and the teammate the list marks.
  const [mention, setMention] = useState<{ start: number; query: string }>();
  const [mentionIndex, setMentionActive] = useState(0);
  const mentionList = useId();
  const asksTeammates = kind === 'note' && teammates.length > 0;
  const mentionOptions = mention && asksTeammates ? matchingTeammates(teammates, mention.query) : [];
  const mentionOpen = Boolean(mention && asksTeammates) && !disabled;
  // Fewer matches as the name is typed keep the mark on the last one.
  const mentionActive = Math.max(0, Math.min(mentionIndex, mentionOptions.length - 1));
  const followCaret = (text: string, caretAt: number | null) => {
    const found = asksTeammates && caretAt !== null ? mentionAt(text, caretAt) : undefined;
    setMention((current) => (found && current && found.start === current.start && found.query === current.query ? current : found));
    if (!found || found.start !== mention?.start) setMentionActive(0);
  };
  const pickMention = (person: Teammate) => {
    if (!mention) return;
    const end = mention.start + 1 + mention.query.length;
    const text = `@${person.name} `;
    caret.current = mention.start + text.length;
    setBody(body.slice(0, mention.start) + text + body.slice(end));
    setMention(undefined);
  };
  const [picking, setPicking] = useState<Picker | null>(null);
  const pickers = replyPickersPart.use();
  const [assist, setAssist] = useState(false);
  // A draft is being written in the open panel: the button says so and keeps the panel (and its Stop) open.
  const [drafting, setDrafting] = useState(false);
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
  // Another part of the app (Autofix's Draft reply) puts a reply in the box, below what is there. It sends one only
  // while its conversation is open, so the box is that conversation's.
  useEffect(() => {
    const put = (event: Event) => {
      const detail = (event as CustomEvent<{ text: string; aiDraftId?: string }>).detail;
      if (!detail || disabled) return;
      if (detail.aiDraftId) return applyDraft(detail.text, 'below', detail.aiDraftId);
      setKind('outgoing');
      insertText(detail.text);
    };
    window.addEventListener('colliedesk:insert-reply', put);
    return () => window.removeEventListener('colliedesk:insert-reply', put);
  });
  const applyDraft = (text: string, how: 'replace' | 'below', id: string, warnings: string[] = []) => {
    setAssist(false);
    setKind('outgoing');
    setAIDraft(id);
    setAIWarnings(warnings);
    const next = how === 'replace' || !body.trim() ? text : `${body.trimEnd()}\n\n${text}`;
    caret.current = next.length;
    setBody(next);
  };
  // A phone gives the conversation the screen: the box is one line with Send reply until the agent goes into
  // it, and stays open while it holds anything (text, files, copies, a list, a notice). It opens when the text
  // box takes the focus (not a button: Chrome focuses one on mousedown, and the compact buttons would go before
  // their click), never on the press itself: a touch's pointerdown comes before its focus, and a box that grew
  // then would move away from the finger. It closes on a press outside (Safari focuses no button, so a blur
  // alone would close it under the finger) or when the keyboard leaves it. components.css styles data-compact
  // below 44 rem only.
  const [active, setActive] = useState(false);
  useEffect(() => {
    if (!active) return;
    const onPress = (event: PointerEvent) => { if (!card.current?.contains(event.target as Node)) setActive(false); };
    document.addEventListener('pointerdown', onPress);
    return () => document.removeEventListener('pointerdown', onPress);
  }, [active]);
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
  // The send shortcut as the keyboard writes it: ⌘↵ on a Mac, Ctrl ↵ elsewhere.
  const sendKeys = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘↵' : 'Ctrl ↵';
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
    // Every active teammate whose @Name the note holds is asked, picked from the list or typed (a draft kept
    // through a reload included); deleting the name takes the ask back.
    const mentions = kind === 'note' ? mentionedIn(sent, teammates) : [];
    setConflict(undefined);
    // The kept draft holds the send's key before the request leaves: a reload meanwhile asks the server about
    // that key before offering the text again, and the same text sent again goes under the same key.
    const input = inputOf(sentFiles, sentCc);
    const key = draftKey ? sendOf(kind, body, input)?.key ?? newSubmitKey() : undefined;
    if (key) {
      pendingSend.current = { kind, body, key, input };
      writeDraft(draftKey, { kind, body, sendKey: key, sendInput: input });
    }
    const send = (draftId?: string) => mentions.length > 0 ? onSubmit?.(kind, sent, sentFiles.length > 0 ? sentFiles : undefined, undefined, undefined, undefined, key, mentions)
      : key ? onSubmit?.(kind, sent, sentFiles.length > 0 ? sentFiles : undefined, sentCc.length > 0 ? sentCc : undefined, seen, draftId, key)
      : draftId ? onSubmit?.(kind, sent, sentFiles, sentCc, seen, draftId)
      : seen !== undefined ? onSubmit?.(kind, sent, sentFiles, sentCc, seen)
      : sentCc.length > 0 ? onSubmit?.(kind, sent, sentFiles, sentCc)
      : sentFiles.length > 0 ? onSubmit?.(kind, sent, sentFiles) : onSubmit?.(kind, sent);
    let result = send(fromDraft);
    if (fromDraft && typeof (result as Promise<unknown> | undefined)?.then === 'function') {
      result = (result as Promise<unknown>).catch((error: unknown) => (draftGone(error) ? send() : Promise.reject(error)));
    }
    if (typeof (result as Promise<unknown> | undefined)?.then !== 'function') { pendingSend.current = undefined; setBody(''); setFiles([]); void resolveNow(); return; }
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
    (result as Promise<unknown>).then(() => {
      if (pendingSend.current?.key === key) pendingSend.current = undefined;
      clearSent();
      return resolveNow();
    }, (error: unknown) => {
      if (error instanceof ReplyConflictError) setConflict(error);
      else setProblem(replyRefusalText(error));
    }).finally(() => { inFlight.current = false; setSending(false); });
  };
  // R starts a reply and N a private note from anywhere on the page, as the keycaps on the tabs say.
  useEffect(() => {
    if (disabled) return;
    const onKey = (event: KeyboardEvent) => {
      if (composing(event) || event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return;
      if (event.key === '/' && maySavedReplies) { event.preventDefault(); setPicking('replies'); return; }
      const next = shortcuts[event.key.toLowerCase()];
      if (!next) return;
      event.preventDefault();
      setKind(next);
      field.current?.focus();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [disabled, setKind, maySavedReplies]);
  const onFieldKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (composing(event.nativeEvent)) return;
    // While the @ list is open the arrows move in it, Enter or Tab picks and Esc closes it; the text stays as typed.
    if (mentionOpen && !event.metaKey && !event.ctrlKey && !event.altKey) {
      const move = { ArrowDown: 1, ArrowUp: -1 }[event.key];
      if (move && mentionOptions.length > 0) { event.preventDefault(); setMentionActive((mentionActive + move + mentionOptions.length) % mentionOptions.length); return; }
      if ((event.key === 'Enter' || event.key === 'Tab') && !event.shiftKey && mentionOptions[mentionActive]) { event.preventDefault(); pickMention(mentionOptions[mentionActive]); return; }
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setMention(undefined); return; }
    }
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); submit(event.shiftKey); }
    if (event.key === '/' && (event.metaKey || event.ctrlKey) && maySavedReplies) { event.preventDefault(); setPicking('replies'); }
  };
  const held = Boolean(body.trim()) || files.length > 0 || picking !== null || assist || ccOpen || ccEdited !== undefined || Boolean(conflict || problem || otherDraft)
    || otherTab || aiWarnings.length > 0;
  const compact = !active && !held;
  const leave = (event: FocusEvent<HTMLDivElement>) => {
    if (event.relatedTarget && !card.current?.contains(event.relatedTarget as Node)) setActive(false);
  };
  return (
    <div className="composer">
      {aiDraftsFor && !disabled && assistPart.status === 'ready' && <assistPart.module.AutoDraftCard conversationId={aiDraftsFor} customerName={customerName}
        timezone={timezone} hasText={Boolean(body.trim())} canManage={canManage} latestCustomerMessage={latestCustomerMessage} onUse={applyDraft}
        onTryAgain={() => setAssist(true)} />}
      <div className="composer-card" data-kind={kind} data-compact={compact} data-writing={active} ref={card} onFocus={(event) => { if (event.target === (field.current as EventTarget | null)) setActive(true); }} onBlur={leave}>
        <div className="composer-head">
          <div className="composer-tabs" role="group" aria-label={messages.messageKind}>
            {mayReply && <button type="button" aria-pressed={kind === 'outgoing'} onClick={() => setKind('outgoing')}>
              <ReplyIcon />{messages.replyMode}<kbd aria-hidden="true">R</kbd></button>}
            <button type="button" aria-pressed={kind === 'note'} onClick={() => setKind('note')}>
              <LockIcon />{messages.noteMode}<kbd aria-hidden="true">N</kbd></button>
          </div>
          <div className="composer-head-end">
            {kind === 'note' && <span className="composer-hint">{asksTeammates ? mentionMessages.noteHint : messages.noteHint}</span>}
          </div>
        </div>
        {aiWarnings.length > 0 && kind === 'outgoing' && (
          <p className="composer-warning" role="alert"><strong>{messages.checkBeforeSending}</strong> {aiWarnings.join(', ')}</p>
        )}
        <textarea ref={field} aria-label={messages.messageBody} value={body} disabled={disabled} onKeyDown={onFieldKey}
          aria-autocomplete={asksTeammates ? 'list' : undefined}
          aria-controls={mentionOpen ? mentionList : undefined} aria-activedescendant={mentionOpen && mentionOptions.length > 0 ? optionIdOf(mentionList, mentionActive) : undefined}
          onBlur={() => setMention(undefined)}
          placeholder={kind === 'note' ? (asksTeammates ? mentionMessages.notePlaceholder : messages.notePlaceholder) : noReplies ? messages.replyNotSendable : messages.replyPlaceholder}
          onChange={(event) => { setBody(event.target.value); setOtherTab(false); editedHere.current = true; followCaret(event.target.value, event.target.selectionStart); }} />
        {mentionOpen && mention && <MentionPicker id={mentionList} options={mentionOptions} query={mention.query} active={mentionActive}
          onActive={setMentionActive} onPick={pickMention} />}
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
        {showCc && <CcField value={cc} onChange={setCcEdited} disabled={disabled || sending} onSend={submit} onBlockingChange={setCcBlocking} />}
        <div className="composer-actions">
          {/* Send leads the row with its shortcut, Send and resolve beside it; the tools follow on the right (I1-a, I7-a). */}
          <button className="primary-action" type="button" disabled={disabled || cannotSend} aria-keyshortcuts="Control+Enter Meta+Enter" onClick={() => submit()}>
            <span className="send-label">{kind === 'note' ? messages.privateNote : messages.reply}</span><kbd className="send-keys" aria-hidden="true">{sendKeys}</kbd><span className="send-icon"><ArrowUpIcon /></span>
          </button>
          {canResolve && <button type="button" className="send-and-resolve" disabled={disabled || cannotSend}
            aria-keyshortcuts="Control+Shift+Enter Meta+Shift+Enter" title={`${messages.sendAndResolve} (Ctrl+Shift+Enter)${resolveTells ? messages.sendAndResolveTells(resolveTells) : ''}`} onClick={() => submit(true)}>
            {messages.sendAndResolve}</button>}
          {/* The one-line box on a phone keeps a private note one press away, as its tab is when the box is open. */}
          {compact && kind === 'outgoing' && <button type="button" className="compact-note" disabled={disabled} aria-label={messages.writeNote}
            onClick={() => { setKind('note'); field.current?.focus(); }}><LockIcon /></button>}
          {/* Attach is the first tool; the chosen files and their notes take a line above the row (components.css). */}
          <span className="composer-tools-start" aria-hidden="true" />
          <AttachmentPicker files={files} onChange={setFiles} limits={attachmentLimits} disabled={disabled || sending} />
          {/* Mouse down must not blur the open list, or the click would close it and open it again. */}
          {maySavedReplies && <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'replies'} title={`${messages.insertSavedReply} (/)`}
            onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('replies')}>
            <BookmarkIcon /><span className="tool-label">{messages.insertSavedReply}</span><kbd aria-hidden="true">/</kbd></button>}
          {mayArticles && <button type="button" className="saved-reply-button" disabled={disabled} aria-haspopup="dialog" aria-expanded={picking === 'articles'} title={messages.articles}
            onMouseDown={(event) => event.preventDefault()} onClick={() => toggle('articles')}>
            <BookIcon /><span className="tool-label">{messages.articles}</span></button>}
          {aiDraftsFor && (
            <button type="button" className="saved-reply-button ai-tool" disabled={disabled} aria-expanded={assist} aria-disabled={assist && drafting ? true : undefined}
              onClick={() => { if (!(assist && drafting)) setAssist((open) => !open); }}>
              <SparklesIcon /><span className="tool-label">{assist && drafting ? messages.drafting : messages.draftWithAI}</span>
              {assistPart.status === 'ready' && !assist && !aiDraft && <assistPart.module.DraftStatus conversationId={aiDraftsFor} agentId={agentId} />}</button>
          )}
          {/* Only an admin can turn drafts on, so only an admin sees the tool while they are off. */}
          {!aiDraftsFor && aiDraftsOff && canManage && (
            // Its name says what it is (the app's own Settings link keeps the name "Settings"); how to turn
            // drafts on is its description.
            <a className="saved-reply-button" href="/settings/ai" target="_blank" rel="noopener" aria-label={messages.aiDraftsOffName} title={messages.aiDraftsOffAdmin}>
              <SparklesIcon /><span className="tool-label">{messages.draftWithAI}</span><span className="ai-off-label">{messages.aiDraftsOff}</span></a>)}
          {kind === 'outgoing' && !showCc && (
            <button type="button" className="cc-button" disabled={disabled} aria-expanded={false} title={messages.ccToggleTitle}
              onClick={() => { focusCc.current = true; setCcOpen(true); }}>{messages.ccToggle}</button>
          )}
        </div>
      </div>
    </div>
  );
}
