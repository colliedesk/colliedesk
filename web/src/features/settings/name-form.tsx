import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';
import { useSaveChanges, useSettingsEdit } from './settings-edits';
import { settingsMessages } from './settings-messages';

interface NameFormProps {
  label: string;
  // saveLabel, for a step that is its own action (a setup step), gives the form a button of its own. Without it the
  // change waits for the page's Save, which names it in the bar.
  saveLabel?: string;
  // hint says where the name shows; it is the field's description.
  hint: string;
  value: string;
  canManage: boolean;
  save?: SaveState;
  onSave?: (name: string) => Promise<unknown> | void;
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
  const feedback = useSaveFeedback(save, errorId, input, name);
  const saveChanges = useSaveChanges();
  useSettingsEdit(`name:${label}`, saveLabel !== undefined || !canManage || name.trim() === value.trim() ? undefined : { label, discard: () => setName(value),
    // The browser says what is wrong with a length it refuses, as a form would; the server judges the rest.
    save: () => {
      if (input.current && !input.current.checkValidity()) { input.current.reportValidity(); return Promise.reject(new Error('invalid')); }
      return onSave?.(name.trim());
    } });
  if (!canManage) return <p><strong>{label}: </strong><span>{value}</span></p>;
  return (
    <form className="form-row" onSubmit={(event) => {
      event.preventDefault();
      if (feedback.pending) return;
      if (saveLabel === undefined) void saveChanges(); else onSave?.(name.trim());
    }}>
      <label className="field"><span>{label}</span>
        <input ref={input} value={name} minLength={minLength} maxLength={maxLength} required aria-invalid={feedback.invalid || undefined}
          aria-describedby={feedback.invalid ? `${errorId} ${hintId}` : hintId} onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
      {saveLabel !== undefined && <button type="submit" disabled={feedback.pending}>{feedback.saveLabel(saveLabel)}</button>}
      {feedback.pending && saveLabel === undefined ? <span className="save-status">{settingsMessages.saving}</span> : feedback.status}
      {feedback.invalid && save?.conflict && conflictLink?.(save.conflict)}
      <small id={hintId}>{hint}</small>
    </form>
  );
}
