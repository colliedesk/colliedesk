import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { ApiError, api } from './api';
import type { Guard, Setting } from './api';
import type { ConsoleData } from './app';
import { CommandLine, when } from './copy';
import { fields, sections } from './fields';
import type { Draft } from './guards';
import { changedKeys, confirmMatches, emptyDraft, mayAskToType, missing, needs, validCode } from './guards';
import { ConsoleDialog } from './dialog';
import { m } from './messages';
import { href, navigate } from './router';

type SetDraft = Dispatch<SetStateAction<Draft>>;

function sourceText(setting: Setting): string {
  const at = when(setting.set_at);
  const by = setting.set_by ?? '';
  switch (setting.source) {
    case 'default': return m.sourceDefault(setting.default ?? '');
    case 'console': return m.sourceConsole(by, at);
    case 'env-import': return m.sourceEnv(at);
    case 'cli': return m.sourceCli(by, at);
    case 'request': return m.sourceRequest(by, at);
    default: return m.sourceOther(setting.source, at);
  }
}

function currentValue(setting: Setting, draft: Draft): string {
  if (setting.key in draft.set) return draft.set[setting.key];
  if (draft.unset.includes(setting.key)) return '';
  return setting.value ?? '';
}

function change(setDraft: SetDraft, setting: Setting, value: string) {
  setDraft((draft) => {
    const set = { ...draft.set };
    const unset = draft.unset.filter((key) => key !== setting.key);
    if (value === (setting.value ?? '') && setting.set) delete set[setting.key];
    else if (value === '' && !setting.set) delete set[setting.key];
    else if (value === '' && setting.set && setting.value !== null) { delete set[setting.key]; unset.push(setting.key); }
    else set[setting.key] = value;
    return { ...draft, set, unset };
  });
}

function SettingRow({ setting, draft, setDraft, error }: { setting: Setting; draft: Draft; setDraft: SetDraft; error?: string }) {
  const info = fields[setting.key] ?? { label: setting.key, kind: 'text' as const };
  const id = `setting-${setting.key.replace(/\./g, '-')}`;
  const noteId = `${id}-note`;
  const dirty = changedKeys(draft).includes(setting.key);
  const locked = setting.locked ?? '';
  const disabled = !setting.editable || locked !== '';
  const [replacing, setReplacing] = useState(false);
  const tags: string[] = [setting.hot ? m.appliesAtOnce : m.appliesInSeconds];
  if (setting.guards.includes('reauth')) tags.push(m.needsYourCode);
  if (setting.guards.includes('type_to_confirm')) tags.push(m.typeToConfirmTag);
  const describedBy = [error && noteId, `${id}-meta`].filter(Boolean).join(' ');
  let control;
  if (info.kind === 'secret' || info.kind === 'pem') {
    if (locked === 'no_key_ring') control = <p className="warning-note">{m.secretLocked}</p>;
    else if (replacing || !setting.set || locked === 'stale_key') {
      const value = draft.secrets[setting.key] ?? '';
      const onChange = (next: string) => setDraft((current) => {
        const secrets = { ...current.secrets };
        if (next) secrets[setting.key] = next; else delete secrets[setting.key];
        return { ...current, secrets };
      });
      control = <div className="secret-edit">
        {locked === 'stale_key' && <p className="warning-note">{m.secretStale}</p>}
        {info.kind === 'pem'
          ? <textarea id={id} rows={5} spellCheck={false} disabled={!setting.editable} value={value} aria-describedby={describedBy} onChange={(event) => onChange(event.target.value)} />
          : <input id={id} type="password" autoComplete="new-password" disabled={!setting.editable} value={value} aria-describedby={describedBy} onChange={(event) => onChange(event.target.value)} />}
        <p className="help">{m.writeOnly}</p>
        {setting.set && <button type="button" className="link-button" onClick={() => { onChange(''); setReplacing(false); }}>{m.keepCurrent}</button>}
      </div>;
    } else {
      control = <div className="secret">
        <span>{m.secretSet(when(setting.set_at), setting.set_by ?? '')}</span>
        <div className="button-row">
          <button type="button" disabled={!setting.editable} onClick={() => setReplacing(true)}>{m.replace}</button>
          <button type="button" className="danger-action" disabled={!setting.editable} onClick={() => setDraft((current) => ({ ...current, unset: [...current.unset.filter((key) => key !== setting.key), setting.key] }))}>{m.remove}</button>
        </div>
      </div>;
    }
  } else if (info.kind === 'switch') {
    const value = currentValue(setting, draft) || setting.default || 'false';
    const on = value === 'true' || value === '1';
    // The authenticator, once required, is turned off on the server only (collie config).
    const oneWay = setting.key === 'operator.require_totp' && on && !(setting.key in draft.set);
    control = <label className="switch"><input id={id} type="checkbox" role="switch" checked={on} disabled={disabled || oneWay} aria-describedby={describedBy}
      onChange={(event) => change(setDraft, setting, event.target.checked ? 'true' : 'false')} /><span>{on ? m.on : m.off}</span></label>;
  } else if (info.kind === 'select') {
    control = <select id={id} value={currentValue(setting, draft)} disabled={disabled} aria-describedby={describedBy} onChange={(event) => change(setDraft, setting, event.target.value)}>
      {(info.options ?? []).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>;
  } else if (info.kind === 'textarea') {
    control = <textarea id={id} rows={3} value={currentValue(setting, draft)} placeholder={setting.default ?? ''} disabled={disabled} aria-describedby={describedBy}
      aria-invalid={error ? true : undefined} onChange={(event) => change(setDraft, setting, event.target.value)} />;
  } else {
    control = <input id={id} type="text" inputMode={info.kind === 'number' ? 'numeric' : undefined} value={currentValue(setting, draft)} placeholder={setting.default ?? ''}
      disabled={disabled} aria-describedby={describedBy} aria-invalid={error ? true : undefined} onChange={(event) => change(setDraft, setting, event.target.value)} />;
  }
  return <div className={`setting field${error ? ' field-bad' : ''}`} data-dirty={dirty || undefined}>
    <label className="field-label" htmlFor={id}>{info.label} {dirty && <span className="badge">{m.changed}</span>}</label>
    <code className="setting-key">{setting.key}</code>
    {control}
    {error && <span className="error-copy field-note" id={noteId}>{error}</span>}
    {info.help && <span className="help field-note">{info.help}</span>}
    <span className="setting-meta" id={`${id}-meta`}>{sourceText(setting)} · {tags.join(' · ')}
      {locked === 'env_off' && <> · {m.telemetryEnvOff}</>}{!setting.editable && <> · {m.notChangeableHere}</>}</span>
  </div>;
}

function asks(draft: Draft, settings: Setting[]): string[] {
  const need = needs(draft, settings);
  const out: string[] = [];
  if (need.reenter.length) out.push(m.askReenter);
  if (mayAskToType(draft, settings)) out.push(m.askConfirm);
  if (need.reauth) out.push(m.askReauth);
  return out;
}

// The products table, on the billing page only.
const ProductsSection = lazy(() => import('./products'));

export function ConfigPage({ group, data, draft, setDraft }: { group: string; data: ConsoleData; draft: Draft; setDraft: SetDraft }) {
  const settings = data.config.settings;
  const byKey = new Map(settings.map((setting) => [setting.key, setting]));
  const [review, setReview] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState<{ version: number; applies: string } | null>(null);
  const [conflict, setConflict] = useState(false);
  const [test, setTest] = useState<{ ok: boolean; message: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const changed = changedKeys(draft);
  const runTest = async () => {
    setTesting(true);
    try {
      const result = await api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets, test: 'smtp' });
      if (!result.ok) setTest({ ok: false, message: Object.values(result.errors).join(' ') });
      else setTest({ ok: result.test?.ok ?? true, message: result.test?.message ?? m.checkOk });
    } catch (error) {
      setTest({ ok: false, message: error instanceof Error ? error.message : m.nothingSaved });
    } finally {
      setTesting(false);
    }
  };
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.groups[group]}</h1><p>{m.groupLead[group]}</p></div></header>
    {saved && <p className="apply-line" role="status"><span className="dot" data-tone="ok" /><span>{saved.applies === 'trial' ? m.savedTrial(saved.version) : m.saved(saved.version, saved.applies === 'at_once')}</span>
      <a className="link-button" href={href('/history')} onClick={(event) => { event.preventDefault(); navigate('/history'); }}>{m.undoInHistory}</a></p>}
    {conflict && <section className="ops-strip" data-tone="danger" role="alert"><span><strong>{m.conflictTitle}.</strong> {m.conflictLead}</span>
      <div className="button-row"><button type="button" className="primary-action" onClick={() => { setConflict(false); setReview(true); }}>{m.applyMine}</button>
        <button type="button" onClick={() => { setConflict(false); setDraft(emptyDraft()); }}>{m.discardMine}</button></div></section>}
    {(sections[group] ?? []).map((section) => <section className="settings-card" key={section.title}>
      <div className="section-heading"><h2>{section.title}</h2><p>{section.lead}</p></div>
      <div className="settings-body">
        {section.keys.map((key) => byKey.get(key)).filter((setting): setting is Setting => Boolean(setting)).map((setting) =>
          <SettingRow key={setting.key} setting={setting} draft={draft} setDraft={setDraft} error={errors[setting.key]} />)}
        {section.test === 'smtp' && <div className="ops-check">
          <button type="button" disabled={testing} onClick={() => void runTest()}>{testing ? m.checking : m.sendTestEmail(data.config.operator_email ?? data.me.operator.email)}</button>
          {test && <p className={test.ok ? 'help' : 'error-copy'} role={test.ok ? 'status' : 'alert'}>{test.message}</p>}
        </div>}
      </div>
    </section>)}
    {group === 'billing' && <Suspense fallback={null}><ProductsSection totp={data.me.operator.totp_enabled} /></Suspense>}
    {changed.length > 0 && <div className="save-bar" role="region" aria-label={m.unsavedChanges(changed.length)}>
      <span><strong>{m.unsavedChanges(changed.length)}</strong>{asks(draft, settings).length > 0 && <> · {m.savingAsksFor} {asks(draft, settings).join(' · ')}</>}</span>
      <div className="button-row"><button type="button" onClick={() => { setDraft(emptyDraft()); setErrors({}); }}>{m.discard}</button>
        <button type="button" className="primary-action" onClick={() => setReview(true)}>{m.reviewAndSave}</button></div>
    </div>}
    {review && <Review data={data} draft={draft} onClose={() => setReview(false)}
      onSaved={(version, applies) => { setReview(false); setDraft(emptyDraft()); setErrors({}); setSaved({ version, applies }); void data.reload(); }}
      onInvalid={(fieldErrors) => { setReview(false); setErrors(fieldErrors); }}
      onConflict={() => { setReview(false); setConflict(true); void data.reload(); }} />}
  </div>;
}

// Review is the one dialog that gathers every check a change needs, in a fixed order: what changes, the
// secret entered again, the phrase typed, the code. Its button works only when everything is filled, and Escape
// only closes it: nothing is applied without the button.
export function Review({ data, draft, onClose, onSaved, onInvalid, onConflict }: {
  data: ConsoleData; draft: Draft; onClose: () => void; onSaved: (version: number, applies: string) => void;
  onInvalid: (errors: Record<string, string>) => void; onConflict: () => void;
}) {
  const settings = data.config.settings;
  const byKey = new Map(settings.map((setting) => [setting.key, setting]));
  // What the change does and asks comes from the server (the same check PATCH runs); until it answers, the
  // review waits and its button stays off.
  const [guards, setGuards] = useState<Guard[] | null>(null);
  // The parent passes a new callback on each render; the check runs once per draft.
  const invalid = useRef(onInvalid);
  useEffect(() => { invalid.current = onInvalid; }, [onInvalid]);
  useEffect(() => {
    let live = true;
    api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets }).then((result) => {
      if (!live) return;
      if (!result.ok) invalid.current(result.errors);
      else setGuards(result.guards ?? []);
    }, () => { if (live) setGuards([]); });
    return () => { live = false; };
  }, [draft]);
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
        {secret ? <div className="diff-vals">{m.replaced}</div> : <div className="diff-vals"><del>{setting?.value || setting?.default || '—'}</del><ins>{after || '—'}</ins></div>}</li>;
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
