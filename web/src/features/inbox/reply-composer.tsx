import { useState } from 'react';
import { messages } from '../../messages';
import { LockIcon } from '../../ui/icons';

interface ReplyComposerProps {
  disabled?: boolean;
  onSubmit?: (kind: 'outgoing' | 'note', body: string) => void;
}

export function ReplyComposer({ disabled, onSubmit }: ReplyComposerProps) {
  const [kind, setKind] = useState<'outgoing' | 'note'>('outgoing');
  const [body, setBody] = useState('');
  const submit = () => {
    if (!body.trim()) return;
    onSubmit?.(kind, body.trim());
    setBody('');
  };
  return (
    <div className="composer">
      <div className="composer-card" data-kind={kind}>
        <div className="composer-tabs" role="group" aria-label={messages.messageBody}>
          <button type="button" aria-pressed={kind === 'outgoing'} onClick={() => setKind('outgoing')}>{messages.replyMode}</button>
          <button type="button" aria-pressed={kind === 'note'} onClick={() => setKind('note')}><LockIcon />{messages.noteMode}</button>
        </div>
        <textarea aria-label={messages.messageBody} value={body} onChange={(event) => setBody(event.target.value)} disabled={disabled} />
        <div className="composer-actions">
          <button className="primary-action" type="button" disabled={disabled || !body.trim()} onClick={submit}>
            {kind === 'note' ? messages.privateNote : messages.reply}
          </button>
        </div>
      </div>
    </div>
  );
}
