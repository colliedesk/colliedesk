import { useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { OperatorRequest } from './api';
import type { ConsoleData } from './app';
import { when } from './copy';
import { ConsoleDialog } from './dialog';
import { fields } from './fields';
import { confirmMatches } from './guards';
import { m } from './messages';
import { pm } from './page-messages';

export function Requests({ data }: { data: ConsoleData }) {
  const [items, setItems] = useState<OperatorRequest[] | null>(null);
  const [open, setOpen] = useState<OperatorRequest | null>(null);
  const load = () => { void api.requests().then((answer) => setItems(answer.items), () => setItems([])); };
  useEffect(load, []);
  if (!items) return <div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div>;
  const waiting = items.filter((item) => item.status === 'pending');
  const decided = items.filter((item) => item.status !== 'pending');
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.requests}</h1><p>{pm.requestsLead}</p></div></header>
    <section className="settings-card"><div className="section-heading"><h2>{pm.waitingForYou(waiting.length)}</h2></div>
      <div className="settings-body">{waiting.length === 0 ? <p className="empty">{pm.noRequests}</p> : <ul className="ops-list">
        {waiting.map((item) => <li key={item.id}><div><strong>{item.summary}</strong><span>{item.token_label} · {item.email} · {when(item.created_at)}</span>
          {item.needs_secrets.length > 0 && <span className="badge">{pm.needsSecret}</span>}</div>
          <button type="button" className="primary-action" onClick={() => setOpen(item)}>{pm.review}</button></li>)}
      </ul>}</div></section>
    {decided.length > 0 && <section className="settings-card"><div className="section-heading"><h2>{pm.decided}</h2></div>
      <div className="settings-body"><ul className="ops-list">{decided.map((item) => <li key={item.id}><div><strong>{item.summary}</strong>
        <span>{pm.status[item.status]}{item.decided_by ? ` · ${item.decided_by}` : ''} · {when(item.decided_at ?? item.created_at)}</span></div></li>)}</ul></div></section>}
    {open && <Decide request={open} data={data} onClose={() => setOpen(null)} onDone={() => { setOpen(null); load(); void data.reload(); }} />}
  </div>;
}

function Decide({ request, data, onClose, onDone }: { request: OperatorRequest; data: ConsoleData; onClose: () => void; onDone: () => void }) {
  const [code, setCode] = useState('');
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [typed, setTyped] = useState<Record<string, string>>({});
  const phrases = (request.guards ?? []).filter((guard) => guard.phrase);
  const previews = (request.guards ?? []).filter((guard) => guard.preview);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const totp = data.me.operator.totp_enabled;
  const ready = (!totp || /^\d{6}$/.test(code)) && request.needs_secrets.every((name) => secrets[name]?.trim())
    && phrases.every((guard) => confirmMatches(typed[guard.key] ?? '', guard.phrase ?? ''));
  const decide = async (action: 'approve' | 'reject') => {
    setBusy(true);
    setError('');
    try {
      await api.decide(request.id, action, { reauth_code: code, ...(action === 'approve' ? { secrets, confirm: typed } : {}) });
      onDone();
    } catch (problem) {
      setError(problem instanceof ApiError ? problem.message : m.nothingSaved);
    } finally {
      setBusy(false);
    }
  };
  return <ConsoleDialog title={request.summary} onClose={onClose} closeLabel={pm.notNow} onSubmit={() => { if (ready) void decide('approve'); }}
    footer={<>
      <button type="button" className="danger-action" disabled={busy || (totp && !/^\d{6}$/.test(code))} onClick={() => void decide('reject')}>{pm.reject}</button>
      <button type="button" onClick={onClose}>{pm.notNow}</button>
      <button type="submit" className="primary-action" disabled={!ready || busy}>{pm.approveAndApply}</button>
    </>}>
    <dl className="ops-env"><div><dt>{pm.askedBy}</dt><dd>{request.token_label} · {request.email}</dd></div><div><dt>{pm.sent}</dt><dd>{when(request.created_at)}</dd></div>
      {request.note && <div><dt>{pm.itsNote}</dt><dd>{request.note}</dd></div>}</dl>
    <RequestChanges request={request} data={data} />
    {previews.length > 0 && <div className="warning-note"><h3>{pm.whatItDoes}</h3><ul>{previews.map((guard) => <li key={guard.key}>{guard.preview}</li>)}</ul></div>}
    {phrases.map((guard) => <div className="field" key={guard.key}>
      <label className="field-label" htmlFor={`decide-confirm-${guard.key}`}>{pm.typeToConfirm(guard.phrase ?? '')}</label>
      <input id={`decide-confirm-${guard.key}`} autoComplete="off" spellCheck={false} value={typed[guard.key] ?? ''} onChange={(event) => setTyped({ ...typed, [guard.key]: event.target.value })} />
    </div>)}
    {request.needs_secrets.map((name) => <div className="field" key={name}><label className="field-label" htmlFor={`secret-${name}`}><code>{name}</code></label>
      <input id={`secret-${name}`} type="password" autoComplete="off" value={secrets[name] ?? ''} onChange={(event) => setSecrets({ ...secrets, [name]: event.target.value })} />
      <span className="help field-note">{pm.secretOnlyYou}</span></div>)}
    {totp && <div className="field"><label className="field-label" htmlFor="decide-code">{pm.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="decide-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} spellCheck={false} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </ConsoleDialog>;
}

// RequestChanges shows what a configuration change asks to set, as the review of a change does; any other
// call shows its body.
function RequestChanges({ request, data }: { request: OperatorRequest; data: ConsoleData }) {
  const body = (request.body ?? {}) as { set?: Record<string, string>; unset?: string[]; secrets?: Record<string, unknown> };
  if (request.route !== 'PATCH /api/v1/operator/config') {
    return request.body ? <pre className="diff">{JSON.stringify(request.body, null, 2)}</pre> : null;
  }
  const byKey = new Map(data.config.settings.map((setting) => [setting.key, setting]));
  const rows = [...Object.entries(body.set ?? {}).map(([key, value]) => [key, value] as const), ...(body.unset ?? []).map((key) => [key, ''] as const)];
  return <ul className="diff">
    {rows.map(([key, after]) => <li className="diff-row" key={key}><div>{fields[key]?.label ?? key}<code className="key">{key}</code></div>
      <div className="diff-vals"><del>{byKey.get(key)?.value || byKey.get(key)?.default || '—'}</del><ins>{after || '—'}</ins></div></li>)}
    {Object.keys(body.secrets ?? {}).map((key) => <li className="diff-row" key={key}><div>{fields[key]?.label ?? key}<code className="key">{key}</code></div>
      <div className="diff-vals">{pm.replaced}</div></li>)}
  </ul>;
}
