import { useEffect, useState } from 'react';
import type { WebFormSettings } from '../../api/schemas';
import { messages } from '../../messages';
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
}

function parseOrigins(text: string): string[] {
  return text.split('\n').map((line) => line.trim()).filter(Boolean);
}

export function WebFormSection({ settings, canManage, save, onSave, part = 'all' }: WebFormSectionProps) {
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
              {!embed && <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => { feedback.touch(); setEnabled(event.target.checked); }} /><span>{messages.webFormAccept}</span></label>}
              <label className="field"><span>{messages.webFormOrigins}</span>
                <textarea rows={3} value={origins} placeholder="https://example.com" onChange={(event) => { feedback.touch(); setOrigins(event.target.value); }} /></label>
              <small className="muted-copy">{messages.webFormOriginsHelp}</small>
              <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(messages.saveWebForm)}</button>
                {feedback.status}</span>
            </form>
          : <ul className="muted-copy">{(settings?.allowed_origins ?? []).map((origin) => <li key={origin}>{origin}</li>)}</ul>}
        {settings && <label className="invite-link"><span className="field-label">{messages.webFormSubmitUrl}</span>
          <input readOnly value={`${window.location.origin}${settings.submit_path}`} /><small>{messages.webFormSubmitHelp}</small></label>}
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
