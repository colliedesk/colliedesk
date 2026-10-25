import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface NameFormProps {
  label: string;
  saveLabel: string;
  // hint says where the name shows; it is the field's description.
  hint: string;
  value: string;
  canManage: boolean;
  save?: SaveState;
  onSave?: (name: string) => void;
  // The server's length rule for this name; the sender name takes 1 to 100 characters.
  minLength?: number;
  maxLength?: number;
  // conflictLink, for a refusal that names another channel (save.conflict), is a link to it.
  conflictLink?: (conflict: NonNullable<SaveState['conflict']>) => ReactNode;
}

// NameForm edits a name that others see, such as the sender name of the email channel or the workspace
// name; a person who cannot change it reads it.
export function NameForm({ label, saveLabel, hint, value, canManage, save, onSave, minLength = 1, maxLength = 100, conflictLink }: NameFormProps) {
  const [name, setName] = useState(value);
  useEffect(() => setName(value), [value]);
  const hintId = useId();
  const errorId = useId();
  const input = useRef<HTMLInputElement>(null);
  const feedback = useSaveFeedback(save, errorId, input);
  if (!canManage) return <p><strong>{label}: </strong><span>{value}</span></p>;
  return (
    <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) onSave?.(name.trim()); }}>
      <label className="field"><span>{label}</span>
        <input ref={input} value={name} minLength={minLength} maxLength={maxLength} required aria-invalid={feedback.invalid || undefined}
          aria-describedby={feedback.invalid ? `${errorId} ${hintId}` : hintId} onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
      <button type="submit" disabled={feedback.pending}>{feedback.saveLabel(saveLabel)}</button>
      {feedback.status}
      {feedback.invalid && save?.conflict && conflictLink?.(save.conflict)}
      <small id={hintId}>{hint}</small>
    </form>
  );
}
