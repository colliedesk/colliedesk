import { useEffect, useState } from 'react';
import { api } from './api';
import type { AuditEntry } from './api';
import { when } from './copy';
import { m } from './messages';

export function Audit() {
  const [items, setItems] = useState<AuditEntry[] | null>(null);
  const [key, setKey] = useState('');
  const [actor, setActor] = useState('');
  const query = () => {
    const params = new URLSearchParams();
    if (key) params.set('key', key);
    if (actor) params.set('actor', actor);
    const text = params.toString();
    return text ? `?${text}` : '';
  };
  useEffect(() => {
    const params = new URLSearchParams();
    if (key) params.set('key', key);
    if (actor) params.set('actor', actor);
    const text = params.toString();
    void api.audit(text ? `?${text}` : '').then((answer) => setItems(answer.items), () => setItems([]));
  }, [key, actor]);
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.audit}</h1><p>{m.auditLead}</p></div>
      <a className="button" href={`/api/v1/operator/audit${query() ? `${query()}&` : '?'}format=csv`} download>{m.exportCsv}</a></header>
    <div className="ops-filters">
      <label className="field"><span className="field-label">{m.filterSetting}</span><input value={key} onChange={(event) => setKey(event.target.value)} spellCheck={false} /></label>
      <label className="field"><span className="field-label">{m.filterWho}</span><input value={actor} onChange={(event) => setActor(event.target.value)} spellCheck={false} /></label>
    </div>
    {!items ? <div className="skeleton" aria-busy="true" /> : items.length === 0 ? <p className="empty">{m.nothingMatches} <button type="button" className="link-button" onClick={() => { setKey(''); setActor(''); }}>{m.clearFilters}</button></p>
      : <div className="ops-table-wrap" tabIndex={0} role="region" aria-label={m.audit}><table className="ops-table">
        <thead><tr><th scope="col">{m.when}</th><th scope="col">{m.who}</th><th scope="col">{m.action}</th><th scope="col">{m.settings}</th><th scope="col">{m.version}</th><th scope="col">{m.address}</th></tr></thead>
        <tbody>{items.map((item) => <tr key={item.id}><td data-label={m.when}>{when(item.at)}</td><td data-label={m.who}>{item.actor}</td><td data-label={m.action}><code>{item.action}</code></td>
          <td data-label={m.settings}>{item.keys.join(', ')}</td><td data-label={m.version}>{item.version_id ? `v${item.version_id}` : ''}</td><td data-label={m.address}>{item.ip ?? ''}</td></tr>)}</tbody></table></div>}
  </div>;
}
