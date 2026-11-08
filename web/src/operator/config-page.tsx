import { createElement, lazy, Suspense, useEffect, useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { ApiError, api } from './api';
import type { Feature, Guard, Setting } from './api';
import type { ConsoleData } from './app';
import { when } from './copy';
import { fields, sections } from './fields';
import type { Draft } from './guards';
import { changedKeys, emptyDraft, mayAskToType, needs } from './guards';
import { m } from './messages';
import { href, navigate } from './router';

type SetDraft = Dispatch<SetStateAction<Draft>>;

// What the server provider's form (ai-provider, loaded on the AI page only) is given: the page's own rows
// and draft helpers, passed in so the console's first bundle exports nothing for it.
export interface ServerProviderProps {
  byKey: Map<string, Setting>; draft: Draft; setDraft: SetDraft; errors: Record<string, string>; groupOf: (key: string) => string | undefined;
  SettingRow: typeof SettingRow; change: typeof change; currentValue: typeof currentValue;
}

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

// What a feature still needs, each a link to the setting's page; a variable of the environment or another
// feature's switch is named as it is.
function FeatureNeeds({ names, groupOf }: { names: string[]; groupOf: (key: string) => string | undefined }) {
  return <ul className="feature-needs">{names.map((name) => {
    const group = groupOf(name);
    const label = name.startsWith('features.') ? m.featureAnotherSwitch(fields[name]?.label ?? name) : fields[name]?.label ?? name;
    return <li key={name}>{group
      ? <a href={href(`/config/${group}`)} onClick={(event) => { event.preventDefault(); navigate(`/config/${group}`); }}>{label}</a>
      : label} <code className="setting-key">{name}</code></li>;
  })}</ul>;
}

function SettingRow({ setting, draft, setDraft, error, feature, groupOf }: { setting: Setting; draft: Draft; setDraft: SetDraft; error?: string; feature?: Feature; groupOf: (key: string) => string | undefined }) {
  const info = fields[setting.key] ?? { label: setting.key, kind: 'text' as const };
  const id = `setting-${setting.key.replace(/\./g, '-')}`;
  const noteId = `${id}-note`;
  const dirty = changedKeys(draft).includes(setting.key);
  const locked = setting.locked ?? '';
  const disabled = !setting.editable || locked !== '';
  const [replacing, setReplacing] = useState(false);
  const [adding, setAdding] = useState(false);
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
        <span>{m.secretIsSet}</span>
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
  } else if (info.kind === 'feature') {
    // Unset runs the feature once its settings are complete; it cannot be turned on before they are, and a
    // feature switched on whose settings went missing is off until they are back.
    const value = currentValue(setting, draft);
    const missingNow = feature?.missing ?? [];
    const on = value !== 'false' && (missingNow.length === 0 || value === 'true');
    const blocked = missingNow.length > 0 && value !== 'true';
    const needsId = `${id}-needs`;
    control = <div className="feature-switch">
      <label className="switch"><input id={id} type="checkbox" role="switch" checked={on} disabled={disabled || blocked}
        aria-describedby={[missingNow.length > 0 && needsId, describedBy].filter(Boolean).join(' ')}
        onChange={(event) => change(setDraft, setting, event.target.checked ? 'true' : 'false')} /><span>{on ? m.on : m.off}</span></label>
      {missingNow.length > 0 && <div id={needsId} className={value === 'true' ? 'warning-note' : 'help'}>
        <span>{value === 'true' ? m.featureIncomplete : m.featureNeeds}</span><FeatureNeeds names={missingNow} groupOf={groupOf} /></div>}
    </div>;
  } else if (info.kind === 'select') {
    // A select with "Other" takes a value of its own in a box under it.
    const value = currentValue(setting, draft) || setting.default || '';
    const own = Boolean(info.other) && !(info.options ?? []).some(([option]) => option === value);
    control = <>
      <select id={id} value={own ? '\u0000' : value} disabled={disabled} aria-describedby={describedBy} onChange={(event) => change(setDraft, setting, event.target.value === '\u0000' ? ' ' : event.target.value)}>
        {(info.options ?? []).map(([option, label]) => <option key={option} value={option}>{label}</option>)}
        {info.other && <option value={'\u0000'}>{info.other}</option>}</select>
      {own && <input type="text" aria-label={info.other} value={value.trim()} disabled={disabled} aria-invalid={error ? true : undefined} onChange={(event) => change(setDraft, setting, event.target.value || ' ')} />}
    </>;
  } else if (info.kind === 'list') {
    // One box per value; the setting keeps them comma separated. A box just added stays until typed in.
    const items = currentValue(setting, draft).split(',').map((item) => item.trim());
    const saved = items.length === 1 && items[0] === '' ? [] : items;
    const rows = adding ? [...saved, ''] : saved;
    const write = (next: string[]) => { setAdding(false); change(setDraft, setting, next.filter((item, at) => item !== '' || at < saved.length).join(',')); };
    control = <div className="list-edit" id={id}>
      {rows.map((item, index) => <div className="list-row" key={index}>
        <input type="text" aria-label={`${info.label} ${index + 1}`} value={item} placeholder={info.placeholder} disabled={disabled} aria-invalid={error ? true : undefined}
          onChange={(event) => write(rows.map((each, at) => (at === index ? event.target.value.replace(/,/g, '') : each)))} />
        <button type="button" className="link-button" aria-label={m.removeItem(item || String(index + 1))} disabled={disabled} onClick={() => write(rows.filter((_, at) => at !== index))}>×</button>
      </div>)}
      <button type="button" aria-label={m.addTo(info.label)} disabled={disabled || rows.some((item) => item === '')} onClick={() => setAdding(true)}>{m.addItem}</button>
    </div>;
  } else if (info.kind === 'bytes') {
    // A size in MB or GB; the setting keeps bytes.
    const bytes = Number(currentValue(setting, draft) || setting.default || 0);
    const gb = bytes >= 1024 ** 3 && bytes % 1024 ** 3 === 0;
    const unit = gb ? 1024 ** 3 : 1024 ** 2;
    const write = (amount: string, size: number) => change(setDraft, setting, amount.trim() === '' ? '' : String(Math.round(Number(amount.replace(',', '.')) * size)));
    control = <div className="size-edit">
      <input id={id} type="text" inputMode="decimal" value={bytes ? String(Math.round((bytes / unit) * 100) / 100) : ''} disabled={disabled} aria-describedby={describedBy} aria-invalid={error ? true : undefined}
        onChange={(event) => write(event.target.value, unit)} />
      <select aria-label={m.unit} value={unit} disabled={disabled} onChange={(event) => write(String(bytes / unit), Number(event.target.value))}>
        <option value={1024 ** 2}>MB</option><option value={1024 ** 3}>GB</option></select>
    </div>;
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
  const changed = new Set(changedKeys(draft));
  if (settings.some((setting) => changed.has(setting.key) && setting.guards.includes('trial'))) out.push(m.askTrial);
  if (need.reauth) out.push(m.askReauth);
  return out;
}

// The products table, on the billing page only.
const ProductsSection = lazy(() => import('./products'));
// The server provider's form, on the AI page only.
const ServerProvider = lazy(() => import('./ai-provider'));
const ServerPricing = lazy(() => import('./ai-provider').then((module) => ({ default: module.ServerPricing })));
// The dialog that asks what a save needs (a code, a secret again, a phrase), only when it needs one.
const Review = lazy(() => import('./review'));

export function ConfigPage({ group, data, draft, setDraft }: { group: string; data: ConsoleData; draft: Draft; setDraft: SetDraft }) {
  const settings = data.config.settings;
  const byKey = new Map(settings.map((setting) => [setting.key, setting]));
  const [review, setReview] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState<{ version: number; applies: string } | null>(null);
  const [conflict, setConflict] = useState(false);
  const [test, setTest] = useState<{ ok: boolean; message: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const [checked, setChecked] = useState<Guard[] | undefined>();
  const [saving, setSaving] = useState(false);
  const changed = changedKeys(draft);
  const done = (version: number, applies: string) => { setReview(false); setDraft(emptyDraft()); setErrors({}); setSaved({ version, applies }); void data.reload(); };
  const invalid = (fieldErrors: Record<string, string>) => {
    setReview(false);
    // An error of a setting on another page, or of none, shows at the top.
    const away = Object.entries(fieldErrors).filter(([key]) => key !== '' && !here.has(key)).map(([, text]) => text);
    setErrors({ ...fieldErrors, ...(away.length ? { '': [fieldErrors[''], ...away].filter(Boolean).join(' ') } : {}) });
    window.setTimeout(() => document.querySelector('.field-bad, .error-copy[role="alert"]')?.scrollIntoView?.({ block: 'center' }), 0);
  };
  // Save checks the change first: an invalid one says why beside its fields, and one that asks for nothing
  // (no code, secret or phrase) is saved at once; only one that does opens the review.
  const save = async () => {
    if (saving) return;
    setSaving(true);
    setErrors({});
    try {
      const result = await api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets });
      if (!result.ok) { invalid(result.errors); return; }
      const guards = result.guards ?? [];
      const need = needs(draft, settings, guards);
      // A code is asked of an operator with an authenticator; without one, the server takes a recent sign-in.
      if ((need.reauth && data.me.operator.totp_enabled) || need.trial || need.reenter.length > 0 || Object.keys(need.confirm).length > 0) { setChecked(guards); setReview(true); return; }
      const savedNow = await api.change({ base_version: data.config.version, set: draft.set, unset: draft.unset, secrets: draft.secrets, confirm: {} });
      done(savedNow.version, savedNow.applies);
    } catch (error) {
      if (error instanceof ApiError && error.code === 'config_changed') { setConflict(true); void data.reload(); return; }
      if (error instanceof ApiError && error.status === 401) return;
      // The server asks for more than the check said (a code): the review asks for it.
      if (error instanceof ApiError && error.code.startsWith('reauth')) { setChecked(undefined); setReview(true); return; }
      if (error instanceof ApiError) { invalid(Object.keys(error.fields).length ? error.fields : { '': error.message }); return; }
      invalid({ '': error instanceof Error ? error.message : m.nothingSaved });
    } finally {
      setSaving(false);
    }
  };
  // A draft spans pages: say how many of its changes are not on this one.
  const here = new Set((sections[group] ?? []).flatMap((section) => section.keys));
  const elsewhere = changed.filter((key) => !here.has(key)).length;
  const runTest = async (kind: 'smtp' | 'polar' | 'jwks' | 'ai') => {
    setTesting(true);
    try {
      const result = await api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets, test: kind });
      if (!result.ok) setTest({ ok: false, message: Object.values(result.errors).join(' ') });
      else setTest({ ok: result.test?.ok ?? true, message: result.test?.message ?? m.checkOk });
    } catch (error) {
      setTest({ ok: false, message: error instanceof Error ? error.message : m.nothingSaved });
    } finally {
      setTesting(false);
    }
  };
  // A saved trial's line goes once the trial is confirmed or reverted.
  const trialRuns = Boolean(data.status.trial);
  useEffect(() => { if (!trialRuns) setSaved((current) => (current?.applies === 'trial' ? null : current)); }, [trialRuns]);
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.groups[group]}</h1><p>{m.groupLead[group]}</p></div></header>
    {errors[''] && <p className="error-copy page-error" role="alert">{errors['']}</p>}
    {saved && <p className="apply-line" role="status"><span className="dot" data-tone="ok" /><span>{saved.applies === 'trial' ? m.savedTrial(saved.version) : m.saved(saved.version, saved.applies === 'at_once')}</span>
      <a className="link-button" href={href('/history')} onClick={(event) => { event.preventDefault(); navigate('/history'); }}>{m.undoInHistory}</a></p>}
    {conflict && <section className="ops-strip" data-tone="danger" role="alert"><span><strong>{m.conflictTitle}.</strong> {m.conflictLead}</span>
      <div className="button-row"><button type="button" className="primary-action" onClick={() => { setConflict(false); void save(); }}>{m.applyMine}</button>
        <button type="button" onClick={() => { setConflict(false); setDraft(emptyDraft()); }}>{m.discardMine}</button></div></section>}
    {(sections[group] ?? []).map((section) => <section className="settings-card" key={section.title}>
      <div className="section-heading"><h2>{section.title}</h2><p>{section.lead}</p></div>
      <div className="settings-body">
        {group === 'ai' && <Suspense fallback={null}>{createElement(section.keys.includes('ai.server.provider') ? ServerProvider : ServerPricing,
          { byKey, draft, setDraft, errors, groupOf: (key: string) => byKey.get(key)?.group, SettingRow, change, currentValue })}</Suspense>}
        {group !== 'ai' && section.keys.map((key) => byKey.get(key)).filter((setting): setting is Setting => Boolean(setting)).map((setting) =>
          <SettingRow key={setting.key} setting={setting} draft={draft} setDraft={setDraft} error={errors[setting.key]}
            feature={data.config.features?.find((feature) => feature.switch_key === setting.key)} groupOf={(key) => byKey.get(key)?.group} />)}
        {section.test && <div className="ops-check">
          <button type="button" disabled={testing} onClick={() => void runTest(section.test!)}>{testing ? m.checking
            : section.test === 'smtp' ? m.sendTestEmail(data.config.operator_email ?? data.me.operator.email) : section.test === 'polar' ? m.tryPolar : section.test === 'ai' ? m.tryAI : m.tryJWKS}</button>
          {test && <p className={test.ok ? 'help' : 'error-copy'} role={test.ok ? 'status' : 'alert'}>{test.message}</p>}
        </div>}
      </div>
    </section>)}
    {group === 'billing' && <Suspense fallback={null}><ProductsSection totp={data.me.operator.totp_enabled} /></Suspense>}
    {changed.length > 0 && <div className="save-bar" role="region" aria-label={m.unsavedChanges(changed.length)}>
      <span><strong>{m.unsavedChanges(changed.length)}</strong>{elsewhere > 0 && <> {m.onOtherPages(elsewhere)}</>}
        {asks(draft, settings).length > 0 && <span className="save-asks"> · {m.savingAsksFor} {asks(draft, settings).join(' · ')}</span>}</span>
      <div className="button-row"><button type="button" onClick={() => { setDraft(emptyDraft()); setErrors({}); }}>{m.discard}</button>
        <button type="button" className="primary-action" disabled={saving} onClick={() => void save()}>{saving ? m.saving : m.save}</button></div>
    </div>}
    {review && <Suspense fallback={null}><Review data={data} draft={draft} checked={checked} onClose={() => setReview(false)} onSaved={done} onInvalid={invalid}
      onConflict={() => { setReview(false); setConflict(true); void data.reload(); }} /></Suspense>}
  </div>;
}

