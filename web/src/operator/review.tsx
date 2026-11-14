import { useEffect, useRef, useState } from 'react';
import { ApiError, api } from './api';
import type { Guard } from './api';
import type { ConsoleData } from './app';
import { CommandLine } from './copy';
import { fields } from './fields';
import type { Draft } from './guards';
import { confirmMatches, missing, needs, validCode } from './guards';
import { ConsoleDialog } from './dialog';
import { m } from './messages';

// Review is the one dialog that gathers every check a change needs, in a fixed order: what changes, the
// secret entered again, the phrase typed, the code. Its button works only when everything is filled, and Escape
// only closes it: nothing is applied without the button.
export default function Review({ data, draft, checked, onClose, onSaved, onInvalid, onConflict }: {
  data: ConsoleData; draft: Draft; checked?: Guard[]; onClose: () => void; onSaved: (version: number, applies: string) => void;
  onInvalid: (errors: Record<string, string>) => void; onConflict: () => void;
}) {
  const settings = data.config.settings;
  const byKey = new Map(settings.map((setting) => [setting.key, setting]));
  // What the change does and asks comes from the server (the same check PATCH runs); until it answers, the
  // review waits and its button stays off.
  // Save already checked the change (checked); the review asks only for what it needs.
  const [guards, setGuards] = useState<Guard[] | null>(checked ?? null);
  // The parent passes a new callback on each render; the check runs once per draft.
  const invalid = useRef(onInvalid);
  useEffect(() => { invalid.current = onInvalid; }, [onInvalid]);
  useEffect(() => {
    if (checked) return undefined;
    let live = true;
    api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets }).then((result) => {
      if (!live) return;
      if (!result.ok) invalid.current(result.errors);
      else setGuards(result.guards ?? []);
    }, () => { if (live) setGuards([]); });
    return () => { live = false; };
  }, [draft, checked]);
  const need = needs(draft, settings, guards ?? []);
  const totp = data.me.operator.totp_enabled;
  const [code, setCode] = useState('');
  const [typed, setTyped] = useState<Record<string, string>>({});
  const [again, setAgain] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [codeError, setCodeError] = useState('');
  const [cli, setCli] = useState(false);
  const lacking = guards === null ? [m.checkingChange] : missing(need, code, typed, again, totp);
  const submit = async () => {
    if (lacking.length || busy) return;
    setBusy(true);
    setCodeError('');
    try {
      const saved = await api.change({ base_version: data.config.version, set: draft.set, unset: draft.unset, secrets: { ...draft.secrets, ...again },
        confirm: typed, reauth_code: need.reauth ? code.replace(/\s/g, '') : undefined });
      onSaved(saved.version, saved.applies);
    } catch (error) {
      if (!(error instanceof ApiError)) { setCodeError(m.keptChanges); return; }
      if (error.code === 'reauth_code_invalid' || error.code === 'reauth_required') { setCodeError(m.keptChanges); setCode(''); return; }
      if (error.code === 'reauth_needs_new_link') { setCli(true); return; }
      if (error.code === 'config_changed') { onConflict(); return; }
      if (error.status === 401) return;
      onInvalid(Object.keys(error.fields).length ? error.fields : { '': error.message });
    } finally {
      setBusy(false);
    }
  };
  return <ConsoleDialog title={m.reviewChanges(need.changed.length)} help={m.willBeSaved(data.config.version + 1)} onClose={onClose} closeLabel={m.cancel} onSubmit={() => void submit()}
    footer={<>
      {lacking.length > 0 && <span className="help" aria-live="polite">{m.fillEveryCheck(lacking.join(', '))}</span>}
      <button type="button" onClick={onClose}>{m.cancel}</button>
      <button type="submit" className="primary-action" disabled={lacking.length > 0 || busy}>{busy ? m.saving : m.saveAndApply}</button>
    </>}>
    <h3>{m.whatChanges}</h3>
    <ul className="diff">{need.changed.map((key) => {
      const setting = byKey.get(key);
      const secret = setting?.secret;
      const after = key in draft.set ? draft.set[key] : draft.unset.includes(key) ? '' : null;
      return <li className="diff-row" key={key}><div>{fields[key]?.label ?? key}<code className="key">{key}</code></div>
        {secret ? <div className="diff-vals">{draft.unset.includes(key) ? m.removed : m.replaced}</div> : <div className="diff-vals"><del>{setting?.value || setting?.default || '—'}</del><ins>{after || '—'}</ins></div>}</li>;
    })}</ul>
    {guards === null && <p className="help" aria-busy="true">{m.checkingChange}…</p>}
    {need.preview.length > 0 && <div className="warning-note"><h3>{m.whatItDoes}</h3><ul>{need.preview.map((text) => <li key={text}>{text}</li>)}</ul></div>}
    {need.reenter.map((secret) => <div className="field" key={secret}>
      <label className="field-label" htmlFor={`again-${secret}`}>{m.enterSecretAgain}: <code>{secret}</code></label>
      <input id={`again-${secret}`} type="password" autoComplete="off" value={again[secret] ?? ''} onChange={(event) => setAgain({ ...again, [secret]: event.target.value })} />
      <span className="help field-note">{m.enterSecretAgainHelp(secret)}</span>
    </div>)}
    {Object.entries(need.confirm).map(([key, phrase]) => <div className="field" key={key}>
      <label className="field-label" htmlFor={`confirm-${key}`}>{m.typeToConfirm(phrase)}</label>
      <input id={`confirm-${key}`} autoComplete="off" spellCheck={false} value={typed[key] ?? ''} aria-invalid={typed[key] && !confirmMatches(typed[key], phrase) ? true : undefined}
        onChange={(event) => setTyped({ ...typed, [key]: event.target.value })} />
    </div>)}
    {need.reauth && totp && <div className="field">
      <label className="field-label" htmlFor="review-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="review-code" inputMode="numeric" autoComplete="one-time-code" maxLength={7} spellCheck={false} value={code}
        aria-invalid={codeError ? true : undefined} aria-describedby={codeError ? 'review-code-error' : undefined} onChange={(event) => setCode(event.target.value)} />
      {codeError && <span className="error-copy field-note" id="review-code-error" role="alert">{codeError}</span>}
      {!validCode(code) && code !== '' && <span className="help field-note">6 digits</span>}
    </div>}
    {(cli || (need.reauth && !totp && codeError)) && <div className="warning-note" role="alert"><p>{m.reauthCLI}</p>
      <CommandLine command={`docker compose exec server /app/collie operator pair --email ${data.me.operator.email}`} /></div>}
    {!need.reauth && codeError && <p className="error-copy" role="alert">{codeError}</p>}
  </ConsoleDialog>;
}
