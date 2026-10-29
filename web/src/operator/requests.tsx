import { useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { OperatorRequest } from './api';
import type { ConsoleData } from './app';
import { when } from './copy';
import { Dialog } from '../ui/dialog';
import { m } from './messages';

export function Requests({ data }: { data: ConsoleData }) {
  const [items, setItems] = useState<OperatorRequest[] | null>(null);
  const [open, setOpen] = useState<OperatorRequest | null>(null);
  const load = () => { void api.requests().then((answer) => setItems(answer.items), () => setItems([])); };
  useEffect(load, []);
  if (!items) return <div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div>;
  const waiting = items.filter((item) => item.status === 'pending');
  const decided = items.filter((item) => item.status !== 'pending');
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.requests}</h1><p>{m.requestsLead}</p></div></header>
    <section className="settings-card"><div className="section-heading"><h2>{m.waitingForYou(waiting.length)}</h2></div>
      <div className="settings-body">{waiting.length === 0 ? <p className="empty">{m.noRequests}</p> : <ul className="ops-list">
        {waiting.map((item) => <li key={item.id}><div><strong>{item.summary}</strong><span>{item.token_label} · {item.email} · {when(item.created_at)}</span>
          {item.needs_secrets.length > 0 && <span className="badge">{m.needsSecret}</span>}</div>
          <button type="button" className="primary-action" onClick={() => setOpen(item)}>{m.review}</button></li>)}
      </ul>}</div></section>
    {decided.length > 0 && <section className="settings-card"><div className="section-heading"><h2>{m.decided}</h2></div>
      <div className="settings-body"><ul className="ops-list">{decided.map((item) => <li key={item.id}><div><strong>{item.summary}</strong>
        <span>{m.status[item.status]}{item.decided_by ? ` · ${item.decided_by}` : ''} · {when(item.decided_at ?? item.created_at)}</span></div></li>)}</ul></div></section>}
    {open && <Decide request={open} data={data} onClose={() => setOpen(null)} onDone={() => { setOpen(null); load(); void data.reload(); }} />}
  </div>;
}

function Decide({ request, data, onClose, onDone }: { request: OperatorRequest; data: ConsoleData; onClose: () => void; onDone: () => void }) {
  const [code, setCode] = useState('');
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const totp = data.me.operator.totp_enabled;
  const ready = (!totp || /^\d{6}$/.test(code)) && request.needs_secrets.every((name) => secrets[name]?.trim());
  const decide = async (action: 'approve' | 'reject') => {
    setBusy(true);
    setError('');
    try {
      await api.decide(request.id, action, { reauth_code: code, ...(action === 'approve' ? { secrets } : {}) });
      onDone();
    } catch (problem) {
      setError(problem instanceof ApiError ? problem.message : m.nothingSaved);
    } finally {
      setBusy(false);
    }
  };
  return <Dialog focusFirst title={request.summary} onClose={onClose} closeLabel={m.notNow} onSubmit={() => { if (ready) void decide('approve'); }}
    footer={<>
      <button type="button" className="danger-action" disabled={busy || (totp && !/^\d{6}$/.test(code))} onClick={() => void decide('reject')}>{m.reject}</button>
      <button type="button" onClick={onClose}>{m.notNow}</button>
      <button type="submit" className="primary-action" disabled={!ready || busy}>{m.approveAndApply}</button>
    </>}>
    <dl className="ops-env"><div><dt>{m.askedBy}</dt><dd>{request.token_label} · {request.email}</dd></div><div><dt>{m.sent}</dt><dd>{when(request.created_at)}</dd></div>
      {request.note && <div><dt>{m.itsNote}</dt><dd>{request.note}</dd></div>}</dl>
    {request.body && <pre className="diff">{JSON.stringify(request.body, null, 2)}</pre>}
    {request.needs_secrets.map((name) => <div className="field" key={name}><label className="field-label" htmlFor={`secret-${name}`}><code>{name}</code></label>
      <input id={`secret-${name}`} type="password" autoComplete="off" value={secrets[name] ?? ''} onChange={(event) => setSecrets({ ...secrets, [name]: event.target.value })} />
      <span className="help field-note">{m.secretOnlyYou}</span></div>)}
    {totp && <div className="field"><label className="field-label" htmlFor="decide-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="decide-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} spellCheck={false} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </Dialog>;
}
