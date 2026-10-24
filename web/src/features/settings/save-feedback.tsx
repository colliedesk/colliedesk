import { useEffect, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';

// SaveState is what a section is told about its latest save request.
export interface SaveState {
  pending?: boolean;
  saved?: boolean;
  // error explains a refused or failed save.
  error?: string;
}

// saveState reads a mutation as the SaveState of its form: a refusal shows the server's words.
export function saveState(m: { isPending: boolean; isSuccess: boolean; isError: boolean; error: unknown }): SaveState {
  return { pending: m.isPending, saved: m.isSuccess, error: m.isError ? (m.error instanceof InvalidRequestError ? m.error.message : messages.settingsActionFailed) : undefined };
}

// useSaveFeedback turns a SaveState into what one form shows. The Saved line
// goes away as soon as the person edits the form again; call touch() on change.
export function useSaveFeedback(save: SaveState = {}) {
  const [edited, setEdited] = useState(false);
  const pending = Boolean(save.pending);
  useEffect(() => { if (pending) setEdited(false); }, [pending]);
  return {
    pending,
    saveLabel: (label: string) => (pending ? messages.saving : label),
    status: <SaveStatus saved={Boolean(save.saved) && !pending && !edited} error={pending ? undefined : save.error} />,
    touch: () => setEdited(true),
  };
}

function SaveStatus({ saved, error }: { saved: boolean; error?: string }) {
  if (error) return <span role="alert" className="error-copy save-status">{error}</span>;
  if (saved) return <span role="status" className="save-status">{messages.saved}</span>;
  return null;
}
