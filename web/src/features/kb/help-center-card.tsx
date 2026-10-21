import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';
import { useSaveFeedback } from '../settings/save-feedback';
import { kbApi } from './kb-api';
import { kbMessages } from './kb-messages';

// HelpCenterSettings is the body of the Help center card in Settings: whether the public pages are on and
// their title. Every agent sees the state; only admins change it, as the server requires.
export function HelpCenterSettings({ canManage, workspaceName }: { canManage: boolean; workspaceName?: string }) {
  const client = useQueryClient();
  const settings = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter });
  const save = useMutation({
    // The language is not edited here yet, so the one stored is sent back as it is.
    mutationFn: ({ enabled, title }: { enabled: boolean; title: string }) => kbApi.saveHelpCenter({ enabled, title, locale: settings.data?.locale || 'en' }),
    onSuccess: (saved) => client.setQueryData(['help-center'], saved),
  });
  const [enabled, setEnabled] = useState(false);
  const [title, setTitle] = useState('');
  useEffect(() => setEnabled(settings.data?.enabled ?? false), [settings.data?.enabled]);
  useEffect(() => setTitle(settings.data?.title ?? ''), [settings.data?.title]);
  // The reply box's articles list opens Settings at #help-center-heading. This card is a chunk of its own,
  // so the browser looked for the heading before the card was there; once it is, go to it.
  useEffect(() => {
    if (settings.isSuccess && window.location.hash === '#help-center-heading') document.getElementById('help-center-heading')?.scrollIntoView();
  }, [settings.isSuccess]);
  const feedback = useSaveFeedback({ pending: save.isPending, saved: save.isSuccess,
    error: save.isError ? (save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed) : undefined });
  if (settings.isError) return <p role="alert" className="error-copy">{kbMessages.helpCenterFailed}</p>;
  if (!settings.data) return <p role="status" className="muted-copy">{messages.loading}</p>;
  const on = settings.data.enabled;
  return <>
    <p role="status" className="saved-state" data-on={on ? 'true' : 'false'}>{on ? kbMessages.helpCenterOn : kbMessages.helpCenterOff}</p>
    <p className="muted-copy">{kbMessages.helpCenterAddress} <a href={settings.data.url} target="_blank" rel="noopener noreferrer">{settings.data.url}</a></p>
    {canManage
      ? <form className="stacked-form" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) save.mutate({ enabled, title: title.trim() }); }}>
          <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => { feedback.touch(); setEnabled(event.target.checked); }} /><span>{kbMessages.helpCenterEnable}</span></label>
          <div className="field"><label htmlFor="help-center-title">{kbMessages.helpCenterTitle}</label>
            <input id="help-center-title" value={title} maxLength={200} placeholder={workspaceName} aria-describedby="help-center-title-help" onChange={(event) => { feedback.touch(); setTitle(event.target.value); }} />
            <small id="help-center-title-help" className="muted-copy">{kbMessages.helpCenterTitleHelp}</small></div>
          <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(kbMessages.saveHelpCenter)}</button>
            {feedback.status}</span>
        </form>
      : <p className="muted-copy">{kbMessages.helpCenterAdminsOnly}</p>}
  </>;
}
