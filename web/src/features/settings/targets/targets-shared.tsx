import type { ReactNode } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ChangedElsewhereError, InvalidRequestError } from '../../../api/client';
import { responseTargetsApi, responseTargetsSchema } from '../../../api/response-targets';
import type { ResponseTargets, ResponseTargetsChange } from '../../../api/response-targets';
import { saveState } from '../save-feedback';
import { targetsMessages as m } from './targets-messages';

// What the cards of Settings → Response targets share: one document the server keeps whole, saved by
// each card with the rest as it was read.

export function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const fields = error.detail?.fields;
  return fields && typeof fields === 'object' ? Object.fromEntries(Object.entries(fields).filter((entry): entry is [string, string] => typeof entry[1] === 'string')) : {};
}

// What a PUT takes: the document without what only the server writes or what is the reader's own, with the
// version it was read at (updated_at): a save after someone else's is refused, 409 changed_elsewhere.
export const withoutStamp = ({ enabled_at: _enabled, my_alert_email: _mine, ...rest }: ResponseTargets): ResponseTargetsChange => rest;

// A card's save state: someone else's save in between is said in words of this page.
export const cardState = (save: Parameters<typeof saveState>[0]) => {
  const state = saveState(save);
  return save.isError && save.error instanceof ChangedElsewhereError ? { ...state, error: m.changedElsewhere } : state;
};

export function useSave() {
  const client = useQueryClient();
  return useMutation({
    // Refused for someone else's save: the page shows theirs; what is being typed stays (the cards keep an edited form).
    onError: (error) => {
      if (!(error instanceof ChangedElsewhereError)) return;
      const current = responseTargetsSchema.safeParse(error.detail?.current);
      if (current.success) client.setQueryData<ResponseTargets>(['response-targets'], (old) => ({ ...current.data, my_alert_email: current.data.my_alert_email ?? old?.my_alert_email }));
      else void client.invalidateQueries({ queryKey: ['response-targets'] });
    },
    mutationFn: (value: ResponseTargetsChange) => responseTargetsApi.set(value),
    // A save answers the workspace's document; the reader's own email choice is not in it, so it is kept.
    onSuccess: (saved) => {
      client.setQueryData<ResponseTargets>(['response-targets'], (current) => ({ ...saved, my_alert_email: saved.my_alert_email ?? current?.my_alert_email }));
      void client.invalidateQueries({ queryKey: ['conversations'] });
    },
  });
}

// useCurrentDocument reads the document as it is now, not as it was when the card last drew. The cards save one
// after the other from one Save, and each must send what the one before it saved.
export function useCurrentDocument(doc: ResponseTargets): () => ResponseTargets {
  const client = useQueryClient();
  return () => client.getQueryData<ResponseTargets>(['response-targets']) ?? doc;
}

export function Card({ id, title, lead, children }: { id: string; title: string; lead?: string; children: ReactNode }) {
  return <section className="settings-card" aria-labelledby={id}>
    <div className="section-heading"><h2 id={id}>{title}</h2>{lead && <p>{lead}</p>}</div>
    <div className="settings-body">{children}</div>
  </section>;
}
