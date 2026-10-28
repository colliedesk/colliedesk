import { useEffect, useId, useState } from 'react';
import { z } from 'zod';
import { InvalidRequestError, request } from '../../api/client';
import type { ReactNode } from 'react';
import type { WebFormSettings } from '../../api/schemas';
import { messages } from '../../messages';
import { channelSetupMessages } from './channel-setup-messages';
import { CopyButton } from './copy-button';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface WebFormSectionProps {
  settings?: WebFormSettings;
  canManage: boolean;
  save?: SaveState;
  onSave?: (enabled: boolean, origins: string[]) => void;
  // part 'embed' is the form as a channel tab shows it: no section frame, and no on/off checkbox, since the
  // channel's own switch turns it on and off.
  part?: 'all' | 'embed';
  // nameField is the form's name, first, where the form is being added.
  nameField?: ReactNode;
}

function parseOrigins(text: string): string[] {
  return text.split('\n').map((line) => line.trim()).filter(Boolean);
}

export function WebFormSection({ settings, canManage, save, onSave, part = 'all', nameField }: WebFormSectionProps) {
  const originsHelp = useId();
  const [enabled, setEnabled] = useState(settings?.enabled ?? false);
  const [origins, setOrigins] = useState((settings?.allowed_origins ?? []).join('\n'));
  useEffect(() => setEnabled(settings?.enabled ?? false), [settings?.enabled]);
  useEffect(() => setOrigins((settings?.allowed_origins ?? []).join('\n')), [settings?.allowed_origins]);
  const feedback = useSaveFeedback(save);
  const state = settings?.enabled ? messages.webFormOn : messages.webFormOff;
  const embed = part === 'embed';
  const body = <div className="settings-body">
        {!embed && <p role="status" className="saved-state" data-on={settings?.enabled ? 'true' : 'false'}>{state}</p>}
        {canManage
          ? <form className="stacked-form" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) onSave?.(enabled, parseOrigins(origins)); }}>
              {nameField}
              {!embed && <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => { feedback.touch(); setEnabled(event.target.checked); }} /><span>{messages.webFormAccept}</span></label>}
              <label className="field"><span>{messages.webFormOrigins}</span>
                <textarea rows={3} value={origins} placeholder="https://www.example.com" aria-describedby={originsHelp} onChange={(event) => { feedback.touch(); setOrigins(event.target.value); }} /></label>
              <small id={originsHelp} className="muted-copy">{channelSetupMessages.webFormOriginsHelp}</small>
              <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(messages.saveWebForm)}</button>
                {feedback.status}</span>
            </form>
          : <ul className="muted-copy">{(settings?.allowed_origins ?? []).map((origin) => <li key={origin}>{origin}</li>)}</ul>}
        {settings && <p>{tryMessages.what}</p>}
        {settings && <TryForm path={settings.submit_path} />}
        {settings && <label className="invite-link"><span className="field-label">{messages.webFormSubmitUrl}</span>
          <input readOnly value={`${window.location.origin}${settings.submit_path}`} /><small>{messages.webFormSubmitHelp}{' '}
            <a href={tryMessages.guideURL} target="_blank" rel="noreferrer">{tryMessages.guide}</a></small></label>}
        {settings && embed && <EmbedCode url={`${window.location.origin}${settings.submit_path}`} />}
        {settings?.legacy_slug && settings.channel_id && <p className="muted-copy">{legacyNote(`${window.location.origin}/api/v1/public/web-forms/${settings.channel_id}`)}</p>}
      </div>;
  if (embed) return body;
  return (
    <section className="settings-card" aria-labelledby="web-form-heading">
      <div className="section-heading"><h2 id="web-form-heading">{messages.webForm}</h2><p>{messages.webFormDescription}</p></div>
      {body}
    </section>
  );
}

const tryMessages = {
  what: 'Your website\'s contact form sends each visitor\'s email and message here, and each one opens a conversation in your inbox. Try it below to see one arrive.',
  guide: 'Read the web form guide',
  guideURL: 'https://colliedesk.com/docs/web-form',
  heading: 'Try the form',
  email: 'Your email',
  message: 'Message',
  messageDefault: 'Hello, this is a test of our web form.',
  send: 'Try the form',
  sending: 'Sending…',
  source: 'Test from Colliedesk',
  arrived: (email: string) => `It arrived: a conversation from ${email} is in your inbox.`,
  openInbox: 'Open the inbox',
  failed: 'The test did not arrive. Check that the form is on, then try again.',
};

// TryForm sends one submission to the form from this page, the way a visitor's browser would, so a team
// sees it work before anyone touches their website. This page is on Colliedesk's own origin, which the
// allowed origins do not need to list.
function TryForm({ path }: { path: string }) {
  const [email, setEmail] = useState('');
  const [message, setMessage] = useState(tryMessages.messageDefault);
  const [state, setState] = useState<{ sending?: boolean; arrived?: string; failed?: string }>({});
  const send = () => {
    if (state.sending) return;
    const from = email.trim();
    setState({ sending: true });
    request(path, z.object({ ok: z.boolean() }), { method: 'POST', body: JSON.stringify({ email: from, message, source: tryMessages.source, website: '' }) })
      .then(() => setState({ arrived: from }), (error: unknown) => setState({ failed: error instanceof InvalidRequestError ? error.message : tryMessages.failed }));
  };
  return <section className="embed-code" aria-labelledby="try-form-heading">
    <h3 id="try-form-heading" className="field-label">{tryMessages.heading}</h3>
    <form className="stacked-form" onSubmit={(event) => { event.preventDefault(); send(); }}>
      <label className="field"><span>{tryMessages.email}</span><input type="text" inputMode="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} /></label>
      <label className="field"><span>{tryMessages.message}</span><textarea rows={2} value={message} onChange={(event) => setMessage(event.target.value)} /></label>
      <span className="button-row"><button type="submit" disabled={state.sending}>{state.sending ? tryMessages.sending : tryMessages.send}</button></span>
    </form>
    {state.arrived && <p role="status">{tryMessages.arrived(state.arrived)} <a href="/">{tryMessages.openInbox}</a></p>}
    {state.failed && <p role="alert" className="error-copy">{state.failed}</p>}
  </section>;
}

// The words of the Embed tab, which the Settings page never had; they load with the channel pages.
const embedMessages = {
  copyUrl: 'Copy submit URL',
  code: 'Code for your page',
  codeHelp: 'Send it from a page on an allowed origin, with the visitor\'s email and message. Leave website empty: a bot that fills it is answered as if stored, and nothing is.',
  copyCode: 'Copy code',
  codeCopied: 'Code copied',
};

// EmbedCode is the submit URL and the request a page sends to it, each with its copy button.
function EmbedCode({ url }: { url: string }) {
  const code = `await fetch('${url}', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email, message, source: 'contact-page', website: '' }),
});`;
  return <>
    <span className="button-row"><CopyButton value={url} copiedText={messages.urlCopied}>{embedMessages.copyUrl}</CopyButton></span>
    <section className="embed-code" aria-labelledby="embed-code-heading">
      <h3 id="embed-code-heading" className="field-label">{embedMessages.code}</h3>
      <pre><code>{code}</code></pre>
      <p className="muted-copy">{embedMessages.codeHelp}</p>
      <span className="button-row"><CopyButton value={code} copiedText={embedMessages.codeCopied}>{embedMessages.copyCode}</CopyButton></span>
    </section>
  </>;
}

// legacyNote: the form with the workspace's older address also answers its own, which a new site may use.
const legacyNote = (own: string) => `The address above is the workspace's older one, which this form keeps. It also has its own, ${own}, which a new site can use.`;
