import { useEffect, useState } from 'react';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

// The server allows this many characters after trimming.
export const displayNameMaxLength = 80;

interface DisplayNameFieldProps {
  value: string;
  // email is shown while the field is empty: it is what the team sees without a name.
  email?: string;
  save?: SaveState;
  onSave?: (name: string) => void;
}

// DisplayNameField sets the name of the person signed in. Saving an empty field clears it.
export function DisplayNameField({ value, email, save, onSave }: DisplayNameFieldProps) {
  const feedback = useSaveFeedback(save);
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  return (
    <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) onSave?.(draft.trim()); }}>
      <label className="field"><span>{settingsMessages.displayName}</span>
        <input value={draft} maxLength={displayNameMaxLength} placeholder={email} autoComplete="name"
          onChange={(event) => { feedback.touch(); setDraft(event.target.value); }} /></label>
      <button type="submit" disabled={feedback.pending}>{feedback.saveLabel(messages.saveName)}</button>
      {feedback.status}
    </form>
  );
}
