import { useEffect, useState } from 'react';
import { api } from './api';
import type { AuditEntry } from './api';
import { when } from './copy';
import { m } from './messages';
import { pm } from './page-messages';

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
    <header className="page-head"><div><h1 id="page-title">{m.audit}</h1><p>{pm.auditLead}</p></div>
      <a className="button" href={`/api/v1/operator/audit${query() ? `${query()}&` : '?'}format=csv`} download>{pm.exportCsv}</a></header>
    <div className="ops-filters">
      <label className="field"><span className="field-label">{pm.filterSetting}</span><input value={key} onChange={(event) => setKey(event.target.value)} spellCheck={false} /></label>
      <label className="field"><span className="field-label">{pm.filterWho}</span><input value={actor} onChange={(event) => setActor(event.target.value)} spellCheck={false} /></label>
    </div>
    {!items ? <div className="skeleton" aria-busy="true" /> : items.length === 0 ? <p className="empty">{pm.nothingMatches} <button type="button" className="link-button" onClick={() => { setKey(''); setActor(''); }}>{pm.clearFilters}</button></p>
      : <div className="ops-table-wrap" tabIndex={0} role="region" aria-label={m.audit}><table className="ops-table">
        <thead><tr><th scope="col">{pm.when}</th><th scope="col">{pm.who}</th><th scope="col">{pm.action}</th><th scope="col">{pm.settings}</th><th scope="col">{pm.version}</th><th scope="col">{pm.address}</th></tr></thead>
        <tbody>{items.map((item) => <tr key={item.id}><td data-label={pm.when} className="ops-when">{when(item.at)}</td><td data-label={pm.who}>{item.actor}</td><td data-label={pm.action}><code>{item.action}</code></td>
          <td data-label={pm.settings}>{item.keys.join(', ')}</td><td data-label={pm.version}>{item.version_id ? `v${item.version_id}` : ''}</td><td data-label={pm.address}>{item.ip ?? ''}</td></tr>)}</tbody></table></div>}
  </div>;
}
