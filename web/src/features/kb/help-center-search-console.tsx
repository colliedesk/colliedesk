import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { InvalidRequestError, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { messages } from '../../messages';
import { searchConsoleMessages as text } from './help-center-search-console-messages';

const path = '/api/v1/help-center/search-console';
const schema = z.object({
  google_site_verification: z.string().nullable(), bing_site_verification: z.string().nullable(),
  content_version: z.number().int(), updated_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'HelpCenterSearchConsole'>>;
// The refusal of a token names its field, so the message shows next to the control.
const refusalFields = z.object({ fields: z.object({}).catchall(z.string()) });

type Tokens = RequestBody<'putHelpCenterSearchConsole'>;

const searchConsoleApi = {
  get: () => request(path, schema),
  save: (tokens: Tokens) => request(path, schema, { method: 'PUT', body: JSON.stringify(tokens) }),
};

function fieldMessages(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const parsed = refusalFields.safeParse(error.detail);
  return parsed.success ? parsed.data.fields : {};
}

// HelpCenterSearchConsoleSection holds the Google and Bing verification tokens of the help center's card.
// Admins paste and save them; every agent sees whether each is set.
export function HelpCenterSearchConsoleSection({ canManage }: { canManage: boolean }) {
  const client = useQueryClient();
  const current = useQuery({ queryKey: ['help-center-search-console'], queryFn: searchConsoleApi.get });
  const save = useMutation({ mutationFn: searchConsoleApi.save, onSuccess: (saved) => client.setQueryData(['help-center-search-console'], saved) });
  const [google, setGoogle] = useState('');
  const [bing, setBing] = useState('');
  useEffect(() => {
    setGoogle(current.data?.google_site_verification ?? '');
    setBing(current.data?.bing_site_verification ?? '');
  }, [current.data?.google_site_verification, current.data?.bing_site_verification]);

  if (current.isError) return <p role="alert" className="error-copy">{text.loadFailed}</p>;
  if (!current.data) return null;
  const fields = fieldMessages(save.error);
  const failed = save.error && Object.keys(fields).length === 0
    ? (save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed) : undefined;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (save.isPending) return;
    save.mutate({ google_site_verification: google.trim() || null, bing_site_verification: bing.trim() || null });
  };
  return <section className="stacked-form" aria-labelledby="help-center-search-console-heading">
    <h3 id="help-center-search-console-heading">{text.heading}</h3>
    <p className="muted-copy">{text.description}</p>
    {canManage
      ? <form className="stacked-form" onSubmit={submit}>
          <Token id="search-console-google" label={text.googleLabel} help={text.googleHelp} value={google} problem={fields.google_site_verification} onChange={setGoogle} />
          <Token id="search-console-bing" label={text.bingLabel} help={text.bingHelp} value={bing} problem={fields.bing_site_verification} onChange={setBing} />
          {failed && <p role="alert" className="error-copy">{failed}</p>}
          <span className="button-row"><button className="primary-action" type="submit" disabled={save.isPending}>{save.isPending ? text.saving : text.save}</button>
            {save.isSuccess && <span role="status" className="saved-state" data-on="true">{text.saved}</span>}</span>
        </form>
      : <>
          <p>{current.data.google_site_verification ? text.set(text.google) : text.notSet(text.google)}</p>
          <p>{current.data.bing_site_verification ? text.set(text.bing) : text.notSet(text.bing)}</p>
          <p className="muted-copy">{text.adminsOnly}</p>
        </>}
  </section>;
}

function Token({ id, label, help, value, problem, onChange }: { id: string; label: string; help: string; value: string; problem?: string; onChange: (value: string) => void }) {
  return <div className="field"><label htmlFor={id}>{label}</label>
    <input id={id} value={value} maxLength={100} autoComplete="off" spellCheck={false} aria-invalid={problem ? true : undefined}
      aria-describedby={`${id}-help`} onChange={(event) => onChange(event.target.value)} />
    <small id={`${id}-help`} className="muted-copy">{help}</small>
    {problem && <small role="alert" className="error-copy">{problem}</small>}</div>;
}
