import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { messages } from '../../messages';
import { LockIcon, ReplyIcon } from '../../ui/icons';

interface ReplyComposerProps {
  disabled?: boolean;
  onSubmit?: (kind: 'outgoing' | 'note', body: string) => void;
}

type Kind = 'outgoing' | 'note';
const shortcuts: Record<string, Kind> = { r: 'outgoing', n: 'note' };

// Typing in any field, or holding a modifier, is text entry and never a shortcut.
function isTyping(target: EventTarget | null) {
  return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName));
}

export function ReplyComposer({ disabled, onSubmit }: ReplyComposerProps) {
  const [kind, setKind] = useState<Kind>('outgoing');
  const [body, setBody] = useState('');
  const field = useRef<HTMLTextAreaElement>(null);
  const submit = () => {
    if (!body.trim()) return;
    onSubmit?.(kind, body.trim());
    setBody('');
  };
  // R starts a reply and N a private note from anywhere on the page, as the keycaps on the tabs say.
  useEffect(() => {
    if (disabled) return;
    const onKey = (event: KeyboardEvent) => {
      const next = shortcuts[event.key.toLowerCase()];
      if (!next || event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return;
      event.preventDefault();
      setKind(next);
      field.current?.focus();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [disabled]);
  const onFieldKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
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
        <div className="composer-actions">
          <span className="composer-hint" aria-hidden="true"><kbd>Ctrl</kbd><kbd>Enter</kbd> {messages.sendShortcut}</span>
          <button className="primary-action" type="button" disabled={disabled || !body.trim()} onClick={submit}>
            {kind === 'note' ? messages.privateNote : messages.reply}
          </button>
        </div>
      </div>
    </div>
  );
}
