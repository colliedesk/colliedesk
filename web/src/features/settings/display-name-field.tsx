import { useEffect, useState } from 'react';
import { settingsMessages } from './settings-messages';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';
import { useSaveChanges, useSettingsEdit } from './settings-edits';

// The server allows this many characters after trimming.
export const displayNameMaxLength = 80;

interface DisplayNameFieldProps {
  value: string;
  // email is shown while the field is empty: it is what the team sees without a name.
  email?: string;
  save?: SaveState;
  onSave?: (name: string) => Promise<unknown> | void;
}

// DisplayNameField sets the name of the person signed in. Saving an empty field clears it. A typed change waits
// for the page's Save (the bar names it), or Enter.
export function DisplayNameField({ value, email, save, onSave }: DisplayNameFieldProps) {
  const feedback = useSaveFeedback(save);
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const saveChanges = useSaveChanges();
  useSettingsEdit('display-name', draft.trim() === value.trim() ? undefined
    : { label: settingsMessages.displayName, save: () => onSave?.(draft.trim()), discard: () => setDraft(value) });
  return (
    <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) void saveChanges(); }}>
      <label className="field"><span>{settingsMessages.displayName}</span>
        <input value={draft} maxLength={displayNameMaxLength} placeholder={email} autoComplete="name"
          onChange={(event) => { feedback.touch(); setDraft(event.target.value); }} /></label>
      {feedback.pending ? <span className="save-status">{settingsMessages.saving}</span> : feedback.status}
    </form>
  );
}
