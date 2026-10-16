import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';
import type { AttachmentLimits } from './attachment-picker';
import { replyRefusalText } from './refusal-text';
import { messages } from '../../messages';
import { LockIcon, ReplyIcon } from '../../ui/icons';

interface ReplyComposerProps {
  disabled?: boolean;
  // May return a promise: the text is kept, with the reason shown, when it rejects.
  // Called with files only when some are chosen.
  onSubmit?: (kind: 'outgoing' | 'note', body: string, files?: File[]) => void | Promise<unknown>;
  // The server's per-message file limits; the composer warns and holds the send while they are exceeded.
  attachmentLimits?: AttachmentLimits;
}

type Kind = 'outgoing' | 'note';
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

export function ReplyComposer({ disabled, onSubmit, attachmentLimits }: ReplyComposerProps) {
  const [kind, setKind] = useState<Kind>('outgoing');
  const [body, setBody] = useState('');
  const [problem, setProblem] = useState<string>();
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const fileProblem = attachmentProblem(files, attachmentLimits);
  const field = useRef<HTMLTextAreaElement>(null);
  // The text leaves the box only after the server accepted it; a refusal keeps it with the reason.
  const submit = () => {
    if (!body.trim() || sending || fileProblem) return;
    const sent = body.trim();
    const sentFiles = files;
    const result = sentFiles.length > 0 ? onSubmit?.(kind, sent, sentFiles) : onSubmit?.(kind, sent);
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
      const next = shortcuts[event.key.toLowerCase()];
      if (!next || composing(event) || event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return;
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
  };
  return (
    <div className="composer">
      <div className="composer-card" data-kind={kind}>
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
        {problem && <p role="alert" className="composer-notice">{problem}</p>}
        <AttachmentPicker files={files} onChange={setFiles} limits={attachmentLimits} disabled={disabled || sending} />
        <div className="composer-actions">
          <span className="composer-hint" aria-hidden="true"><kbd>Ctrl</kbd><kbd>Enter</kbd> {messages.sendShortcut}</span>
          <button className="primary-action" type="button" disabled={disabled || sending || !body.trim() || Boolean(fileProblem)} onClick={submit}>
            {kind === 'note' ? messages.privateNote : messages.reply}
          </button>
        </div>
      </div>
    </div>
  );
}
