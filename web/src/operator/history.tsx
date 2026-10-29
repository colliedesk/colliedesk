import { useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { Guard, Version } from './api';
import type { ConsoleData } from './app';
import { when } from './copy';
import { ConsoleDialog } from './dialog';
import { fields } from './fields';
import { confirmMatches } from './guards';
import { m } from './messages';

export function History({ data }: { data: ConsoleData }) {
  const [versions, setVersions] = useState<Version[] | null>(null);
  const [selected, setSelected] = useState<number | null>(null);
  const [restoring, setRestoring] = useState(false);
  const load = () => { void api.versions().then((answer) => { setVersions(answer.items); setSelected((current) => current ?? answer.items[0]?.id ?? null); }, () => setVersions([])); };
  useEffect(load, []);
  if (!versions) return <div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div>;
  const version = versions.find((item) => item.id === selected);
  const running = data.status.desired_version;
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.history}</h1><p>{m.historyLead}</p></div></header>
    {versions.length <= 1 && <p className="empty">{m.firstSave}</p>}
    <div className="version-layout">
      <ul className="version-list" aria-label={m.history}>{versions.map((item) => <li key={item.id}>
        <button type="button" aria-pressed={item.id === selected} onClick={() => setSelected(item.id)}>
          <strong>v{item.id}</strong><span>{item.source} · {item.created_by} · {when(item.created_at)}</span>
          {item.id === running && <span className="badge">{m.badgeRunning}</span>}{item.status === 'rejected' && <span className="badge" data-tone="danger">{m.badgeRefused}</span>}
        </button></li>)}</ul>
      {version && <section className="version-detail" aria-live="polite">
        <h2>v{version.id}</h2>{version.note && <p>{version.note}</p>}{version.rejected_reason && <p className="error-copy">{version.rejected_reason}</p>}
        <ul className="diff">{version.changes.map((item) => <li className="diff-row" key={item.key}><div>{fields[item.key]?.label ?? item.key}<code className="key">{item.key}</code></div>
          {item.secret ? <div className="diff-vals">{m.replaced}</div> : <div className="diff-vals"><del>{item.before ?? '—'}</del><ins>{item.after ?? '—'}</ins></div>}</li>)}</ul>
        {version.id !== running && <button type="button" onClick={() => setRestoring(true)}>{m.restoreVersion(version.id)}</button>}
      </section>}
    </div>
    {restoring && version && <Restore data={data} version={version.id} undone={versions.filter((item) => item.id > version.id)} onClose={() => setRestoring(false)} onDone={() => { setRestoring(false); load(); void data.reload(); }} />}
  </div>;
}

// Restore says what it undoes: the settings every newer version changed go back to their value in v.
function Restore({ data, version, undone, onClose, onDone }: { data: ConsoleData; version: number; undone: Version[]; onClose: () => void; onDone: () => void }) {
  const keys = [...new Set(undone.flatMap((item) => item.changes.map((change) => change.key)))].sort();
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  // What restoring does and the phrases it asks come from the server, on the first try (no code is spent).
  const [guards, setGuards] = useState<Guard[]>([]);
  const [typed, setTyped] = useState<Record<string, string>>({});
  const totp = data.me.operator.totp_enabled;
  const phrases = guards.filter((guard) => guard.phrase);
  const submit = async () => {
    try {
      await api.restore(version, { base_version: data.config.version, reauth_code: code, confirm: typed });
      onDone();
    } catch (problem) {
      if (problem instanceof ApiError && problem.code === 'confirm_mismatch' && Array.isArray(problem.body.guards)) {
        setGuards(problem.body.guards as Guard[]);
        setError('');
        return;
      }
      setError(problem instanceof ApiError ? problem.message : m.nothingSaved);
    }
  };
  return <ConsoleDialog title={m.restoreAs(version, data.config.version + 1)} onClose={onClose} closeLabel={m.cancel} onSubmit={() => void submit()}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={(totp && !/^\d{6}$/.test(code)) || phrases.some((guard) => !confirmMatches(typed[guard.key] ?? '', guard.phrase ?? ''))}>{m.restoreVersion(version)}</button></>}>
    {keys.length > 0 && <><h3>{m.whatChanges}</h3><p className="help">{m.restoreUndoes(undone.map((item) => `v${item.id}`).join(', '))}</p>
      <ul className="diff">{keys.map((key) => <li className="diff-row" key={key}><div>{fields[key]?.label ?? key}<code className="key">{key}</code></div>
        <div className="diff-vals">{m.backToValueOf(version)}</div></li>)}</ul></>}
    {guards.some((guard) => guard.preview) && <div className="warning-note"><h3>{m.whatItDoes}</h3><ul>{guards.filter((guard) => guard.preview).map((guard) => <li key={guard.key}>{guard.preview}</li>)}</ul></div>}
    {phrases.map((guard) => <div className="field" key={guard.key}>
      <label className="field-label" htmlFor={`restore-confirm-${guard.key}`}>{m.typeToConfirm(guard.phrase ?? '')}</label>
      <input id={`restore-confirm-${guard.key}`} autoComplete="off" spellCheck={false} value={typed[guard.key] ?? ''} onChange={(event) => setTyped({ ...typed, [guard.key]: event.target.value })} />
    </div>)}
    {totp && <div className="field"><label className="field-label" htmlFor="restore-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="restore-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </ConsoleDialog>;
}
