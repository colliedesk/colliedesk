import { useEffect, useId, useState } from 'react';
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
}

// NameForm edits a name of 1 to 100 characters that others see, such as the sender name of the email
// channel or the workspace name; a person who cannot change it reads it.
export function NameForm({ label, saveLabel, hint, value, canManage, save, onSave }: NameFormProps) {
  const [name, setName] = useState(value);
  useEffect(() => setName(value), [value]);
  const feedback = useSaveFeedback(save);
  const hintId = useId();
  if (!canManage) return <p><strong>{label}: </strong><span>{value}</span></p>;
  return (
    <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) onSave?.(name.trim()); }}>
      <label className="field"><span>{label}</span>
        <input value={name} maxLength={100} required aria-describedby={hintId} onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
      <button type="submit" disabled={feedback.pending}>{feedback.saveLabel(saveLabel)}</button>
      {feedback.status}
      <small id={hintId}>{hint}</small>
    </form>
  );
}
