import { useRef, useState } from 'react';
import type { ClipboardEvent, KeyboardEvent } from 'react';
import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';

// The server accepts at most this many (contracts/openapi.yaml, createMessage).
export const maxCc = 10;

// A bare addr-spec; the server decides what it accepts, this only keeps obvious typos from being sent.
const plainAddress = /^[^\s@<>,;"()[\]\\]+@[^\s@<>,;"()[\]\\]+$/;
const separators = /[\s,;]+/;
const commitKeys = [',', ';', ' '];

interface CcFieldProps {
  value: string[];
  onChange: (value: string[]) => void;
  disabled?: boolean;
  // Ctrl/Cmd+Enter in an empty box sends the reply, as it does in the message box.
  onSend?: () => void;
}

export function CcField({ value, onChange, disabled, onSend }: CcFieldProps) {
  const [draft, setDraft] = useState('');
  const [problem, setProblem] = useState<string>();
  const field = useRef<HTMLInputElement>(null);
  // Adds every address in the text. Returns false and keeps the text when one cannot be added.
  const commit = (text: string): boolean => {
    const wanted = text.split(separators).filter(Boolean).map((item) => item.toLowerCase());
    if (wanted.length === 0) return true;
    const next = [...value];
    for (const address of wanted) {
      if (!plainAddress.test(address) || address.length > 254) { setProblem(messages.ccInvalid); return false; }
      if (next.includes(address)) continue;
      if (next.length >= maxCc) { setProblem(messages.ccTooMany(maxCc)); return false; }
      next.push(address);
    }
    setProblem(undefined);
    setDraft('');
    if (next.length !== value.length) onChange(next);
    return true;
  };
  const remove = (address: string) => {
    onChange(value.filter((item) => item !== address));
    field.current?.focus();
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    const sends = event.key === 'Enter' && (event.ctrlKey || event.metaKey);
    if (sends && draft.trim() === '') {
      event.preventDefault();
      onSend?.();
    } else if ((event.key === 'Enter' || commitKeys.includes(event.key)) && draft.trim() !== '') {
      // With text typed, Ctrl+Enter only finishes the address; the next one sends.
      event.preventDefault();
      commit(draft);
    } else if (event.key === 'Backspace' && draft === '' && value.length > 0) {
      event.preventDefault();
      onChange(value.slice(0, -1));
    }
  };
  const onPaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const pasted = event.clipboardData.getData('text');
    if (!separators.test(pasted.trim())) return;
    event.preventDefault();
    commit(draft + pasted);
  };
  return (
    <div className="composer-cc">
      <span className="cc-label" aria-hidden="true">{messages.ccLabel}</span>
      <ul className="cc-chips" aria-label={messages.ccLabel}>
        {value.map((address) => (
          <li key={address}>
            <span>{address}</span>
            <button type="button" disabled={disabled} aria-label={messages.removeCc(address)} title={messages.removeCc(address)} onClick={() => remove(address)}><XIcon /></button>
          </li>
        ))}
      </ul>
      <input ref={field} type="text" inputMode="email" autoComplete="off" spellCheck={false} disabled={disabled} value={draft}
        aria-label={messages.addCc} aria-invalid={problem ? true : undefined} placeholder={value.length === 0 ? 'name@example.com' : undefined}
        onChange={(event) => { setDraft(event.target.value); setProblem(undefined); }} onKeyDown={onKeyDown} onPaste={onPaste}
        onBlur={() => { if (draft.trim() !== '') commit(draft); }} />
      {problem && <p role="alert" className="composer-attachments-problem">{problem}</p>}
    </div>
  );
}
