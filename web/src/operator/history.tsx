import { useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { Version } from './api';
import type { ConsoleData } from './app';
import { when } from './copy';
import { Dialog } from '../ui/dialog';
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
        <ul className="diff">{version.changes.map((item) => <li key={item.key}><code>{item.key}</code> {item.secret ? <span>{m.replaced}</span>
          : <span><del>{item.before ?? '—'}</del> → <ins>{item.after ?? '—'}</ins></span>}</li>)}</ul>
        {version.id !== running && <button type="button" onClick={() => setRestoring(true)}>{m.restoreVersion(version.id)}</button>}
      </section>}
    </div>
    {restoring && version && <Restore data={data} version={version.id} onClose={() => setRestoring(false)} onDone={() => { setRestoring(false); load(); void data.reload(); }} />}
  </div>;
}

function Restore({ data, version, onClose, onDone }: { data: ConsoleData; version: number; onClose: () => void; onDone: () => void }) {
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const totp = data.me.operator.totp_enabled;
  const submit = async () => {
    try {
      await api.restore(version, { base_version: data.config.version, reauth_code: code });
      onDone();
    } catch (problem) {
      setError(problem instanceof ApiError ? problem.message : m.nothingSaved);
    }
  };
  return <Dialog focusFirst title={m.restoreAs(version, data.config.version + 1)} onClose={onClose} closeLabel={m.cancel} onSubmit={() => void submit()}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={totp && !/^\d{6}$/.test(code)}>{m.restoreVersion(version)}</button></>}>
    {totp && <div className="field"><label className="field-label" htmlFor="restore-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="restore-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </Dialog>;
}
