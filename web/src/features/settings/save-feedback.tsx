import { useEffect, useState } from 'react';
import { ConflictError, InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';

// SaveState is what a section is told about its latest save request.
export interface SaveState {
  pending?: boolean;
  saved?: boolean;
  // error explains a refused or failed save.
  error?: string;
  // conflict names the channel that has the value refused (409 channel_name_taken, support_address_taken).
  conflict?: { channel_id: string; channel_name: string };
}

// saveState reads a mutation as the SaveState of its form: a refusal shows the server's words.
export function saveState(m: { isPending: boolean; isSuccess: boolean; isError: boolean; error: unknown }): SaveState {
  const detail = m.isError && m.error instanceof ConflictError ? m.error.detail : undefined;
  const conflict = detail && typeof detail.conflict === 'object' && detail.conflict ? detail.conflict as SaveState['conflict'] : undefined;
  return { pending: m.isPending, saved: m.isSuccess, error: m.isError ? (m.error instanceof InvalidRequestError ? m.error.message : messages.settingsActionFailed) : undefined, conflict };
}

// useSaveFeedback turns a SaveState into what one form shows. The Saved line
// goes away as soon as the person edits the form again; call touch() on change.
export function useSaveFeedback(save: SaveState = {}, errorID?: string) {
  const [edited, setEdited] = useState(false);
  const pending = Boolean(save.pending);
  const error = pending ? undefined : save.error;
  // A new save, or a refusal that came back before its pending state was ever drawn, is about what is
  // in the form now.
  useEffect(() => { if (pending || error) setEdited(false); }, [pending, error]);
  return {
    pending,
    // invalid marks the field the error is about, until the person edits it again.
    invalid: Boolean(error) && !edited,
    saveLabel: (label: string) => (pending ? messages.saving : label),
    status: <SaveStatus saved={Boolean(save.saved) && !pending && !edited} error={error} id={errorID} />,
    touch: () => setEdited(true),
  };
}

function SaveStatus({ saved, error, id }: { saved: boolean; error?: string; id?: string }) {
  if (error) return <span role="alert" id={id} className="error-copy save-status">{error}</span>;
  if (saved) return <span role="status" className="save-status">{messages.saved}</span>;
  return null;
}
