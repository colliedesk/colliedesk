import { useEffect, useState } from 'react';
import type { WebFormSettings } from '../../api/schemas';
import { messages } from '../../messages';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface WebFormSectionProps {
  settings?: WebFormSettings;
  canManage: boolean;
  save?: SaveState;
  onSave?: (enabled: boolean, origins: string[]) => void;
}

function parseOrigins(text: string): string[] {
  return text.split('\n').map((line) => line.trim()).filter(Boolean);
}

export function WebFormSection({ settings, canManage, save, onSave }: WebFormSectionProps) {
  const [enabled, setEnabled] = useState(settings?.enabled ?? false);
  const [origins, setOrigins] = useState((settings?.allowed_origins ?? []).join('\n'));
  useEffect(() => setEnabled(settings?.enabled ?? false), [settings?.enabled]);
  useEffect(() => setOrigins((settings?.allowed_origins ?? []).join('\n')), [settings?.allowed_origins]);
  const feedback = useSaveFeedback(save);
  const state = settings?.enabled ? messages.webFormOn : messages.webFormOff;
  return (
    <section className="settings-card" aria-labelledby="web-form-heading">
      <div className="section-heading"><h2 id="web-form-heading">{messages.webForm}</h2><p>{messages.webFormDescription}</p></div>
      <div className="settings-body">
        <p role="status"><strong>{state}</strong></p>
        {canManage
          ? <form onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) onSave?.(enabled, parseOrigins(origins)); }}>
              <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => { feedback.touch(); setEnabled(event.target.checked); }} /><span>{messages.webFormAccept}</span></label>
              <label className="field"><span>{messages.webFormOrigins}</span>
                <textarea rows={3} value={origins} placeholder="https://example.com" onChange={(event) => { feedback.touch(); setOrigins(event.target.value); }} /></label>
              <small>{messages.webFormOriginsHelp}</small>
              <button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(messages.saveWebForm)}</button>
              {feedback.status}
            </form>
          : <ul className="muted-copy">{(settings?.allowed_origins ?? []).map((origin) => <li key={origin}>{origin}</li>)}</ul>}
        {settings && <label className="invite-link"><span className="field-label">{messages.webFormSubmitUrl}</span>
          <input readOnly value={`${window.location.origin}${settings.submit_path}`} /><small>{messages.webFormSubmitHelp}</small></label>}
      </div>
    </section>
  );
}
