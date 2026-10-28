import { useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { ConflictError, InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';

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
// field, when given, takes the focus when a save is refused: the words of the refusal describe it.
// value, when given, is what the field holds now: a refusal is about the value it refused, so its words
// go while the field holds another one and come back if it holds that one again.
export function useSaveFeedback(save: SaveState = {}, errorID?: string, field?: RefObject<HTMLElement | null>, value?: string) {
  const [edited, setEdited] = useState(false);
  const [refused, setRefused] = useState<{ error: string; value?: string }>();
  const pending = Boolean(save.pending);
  const error = pending ? undefined : save.error;
  // A new save, or a refusal that came back before its pending state was ever drawn, is about what is
  // in the form now.
  useEffect(() => { if (pending || error) setEdited(false); }, [pending, error]);
  useEffect(() => { if (error) field?.current?.focus(); }, [error, field]);
  // The value a refusal is about is the one the form held when it came back.
  const current = useRef(value);
  current.current = value;
  useEffect(() => { setRefused(error ? { error, value: current.current } : undefined); }, [error]);
  const shown = value === undefined ? error : error && refused?.error === error && refused.value === value ? error : undefined;
  return {
    pending,
    // invalid marks the field the error is about: until the person edits it, or while it holds the refused value.
    invalid: value === undefined ? Boolean(error) && !edited : Boolean(shown),
    saveLabel: (label: string) => (pending ? settingsMessages.saving : label),
    status: <SaveStatus saved={Boolean(save.saved) && !pending && !edited} error={shown} id={errorID} />,
    touch: () => setEdited(true),
  };
}

function SaveStatus({ saved, error, id }: { saved: boolean; error?: string; id?: string }) {
  if (error) return <span role="alert" id={id} className="error-copy save-status">{error}</span>;
  if (saved) return <span role="status" className="save-status">{settingsMessages.saved}</span>;
  return null;
}
