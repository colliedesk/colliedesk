import type { ConsoleData } from './app';
import { when } from './copy';
import { m } from './messages';

function warningText(warning: ConsoleData['status']['warnings'][number]): string {
  const env = warning.env_name ?? '';
  switch (warning.code) {
    case 'env_differs': return m.warning.env_differs(env);
    case 'env_same': return m.warning.env_same(env);
    case 'env_dead': return m.warning.env_dead(env);
    case 'no_key_ring': return m.warning.no_key_ring(env);
    case 'secret_stale_key': return m.warning.secret_stale_key(warning.key ?? '');
    case 'version_refused': return m.warning.version_refused(warning.version ?? 0, warning.detail ?? '');
    case 'waiting_for_import': return m.warning.waiting_for_import(warning.detail ?? '');
    case 'path_mode': return m.warning.path_mode(env);
    case 'requests_waiting': return m.warning.requests_waiting(warning.detail ?? '');
    default: return warning.code;
  }
}

export function Overview({ data }: { data: ConsoleData }) {
  const { status, config } = data;
  const on = status.processes.filter((process) => process.running_version === status.desired_version).length;
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.overview}</h1><p>{m.overviewLead}</p></div></header>
    <div className="ops-tiles">
      <section className="ops-tile"><h2>{m.server}</h2><p><span>{m.binary}</span><code>{status.binary_version}</code></p><p><span>{m.migration}</span><code>{status.migration.replace(/\.up\.sql$/, '')}</code></p></section>
      <section className="ops-tile"><h2>{m.configuration}</h2><p><span>{m.desired}</span><code>v{status.desired_version}</code></p><p><span>{m.running}</span><code>{on}/{status.processes.length}</code></p></section>
      <section className="ops-tile"><h2>{m.processes}</h2><p><span>{m.processes}</span><code>{status.processes.length}</code></p></section>
    </div>
    <section className="settings-card"><div className="section-heading"><h2>{m.needsAttention}</h2></div>
      <div className="settings-body">
        {status.warnings.length === 0 ? <p>{m.nothingNeedsAttention}</p> : <ul className="ops-warnings">
          {status.warnings.map((warning, index) => <li key={index} data-tone={warning.severity === 'error' ? 'danger' : 'warn'}><span className="dot" data-tone={warning.severity === 'error' ? 'danger' : 'warn'} />{warningText(warning)}</li>)}
        </ul>}
      </div>
    </section>
    <section className="settings-card"><div className="section-heading"><h2>{m.processes}</h2></div>
      <div className="settings-body ops-table-wrap" tabIndex={0} role="region" aria-label={m.processes}>
        <table className="ops-table"><thead><tr><th scope="col">{m.host}</th><th scope="col">{m.binary}</th><th scope="col">{m.running}</th><th scope="col">{m.heartbeat}</th><th scope="col">{m.waitingFor}</th></tr></thead>
          <tbody>{status.processes.map((process) => <tr key={process.instance_id}>
            <td data-label={m.host}><code>{process.hostname}</code></td><td data-label={m.binary}><code>{process.binary_version}</code></td>
            <td data-label={m.running}><span className="dot" data-tone={process.running_version === status.desired_version ? 'ok' : undefined} /> v{process.running_version}</td>
            <td data-label={m.heartbeat}>{when(process.heartbeat_at)}</td><td data-label={m.waitingFor}>{process.waiting_for || process.last_error || '—'}</td></tr>)}</tbody></table>
      </div>
    </section>
    <section className="settings-card"><div className="section-heading"><h2>{m.environment}</h2><p>{m.environmentLead}</p></div>
      <div className="settings-body"><dl className="ops-env">{config.bootstrap.map((item) => <div key={item.env_name}><dt><code>{item.env_name}</code></dt><dd>{item.set ? <code>{item.value}</code> : m.notSet}</dd></div>)}</dl></div>
    </section>
  </div>;
}
