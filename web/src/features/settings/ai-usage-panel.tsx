import { useId } from 'react';
import type { AIUsage } from '../../api/ai';
import { HelpTip } from '../../ui/help-tip';
import { aiMessages as m } from './ai-messages';
import { money } from './ai-level-dialogs';
import { Meter } from './ai-meter';

const tokens = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(2)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : String(n));

// recentMonths lists this UTC month and the five before it, as YYYY-MM.
export function recentMonths(now: Date = new Date()): string[] {
  return Array.from({ length: 6 }, (_, back) => {
    const at = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - back, 1));
    return `${at.getUTCFullYear()}-${String(at.getUTCMonth() + 1).padStart(2, '0')}`;
  });
}

export function Usage({ usage, failed, month, onMonth }: { usage?: AIUsage; failed?: boolean; month: string; onMonth: (month: string) => void }) {
  const id = useId();
  return <>
    <section className="panel ai-usage-top" aria-label={m.thisMonth}>
      <label className="field" htmlFor={id}><span className="sr-only">{m.month}</span>
        <select id={id} value={month} onChange={(event) => onMonth(event.target.value)}>
          {recentMonths().map((value) => <option key={value} value={value}>{monthName(value)}</option>)}
        </select></label>
      {usage?.allowance ? <Meter allowance={usage.allowance} /> : <p className="muted-copy">{m.noAllowance}</p>}
    </section>
    {failed && <p role="alert" className="error-copy">{m.usageFailed}</p>}
    <section className="panel ai-usage" aria-label={m.usageByFeature} aria-busy={!usage && !failed}>
      {usage && (usage.rows.length === 0 ? <p className="ai-prov-empty muted-copy">{m.noUsage}</p> : <table className="ai-usage-table">
        <thead><tr><th scope="col">{m.columnFeature}</th><th scope="col" className="ai-n">{m.calls}</th><th scope="col" className="ai-n ai-hide-sm">{m.failed}</th>
          <th scope="col" className="ai-n ai-hide-sm">{m.tokens}</th><th scope="col" className="ai-n">{m.cost}</th><th scope="col" className="ai-hide-sm">{m.paidBy}</th></tr></thead>
        <tbody>{usage.rows.map((row) => <tr key={`${row.feature}-${row.billed_to}-${row.provider_label}`}>
          <td>{m.feature[row.feature]}<small>{row.provider_label}{row.automatic > 0 ? ` · ${m.automaticCalls(row.automatic)}` : ''}</small></td>
          <td className="ai-n">{row.calls}</td><td className="ai-n ai-hide-sm">{row.failed}</td>
          <td className="ai-n ai-hide-sm">{tokens(row.input_tokens)} · {tokens(row.output_tokens)}</td>
          <td className="ai-n">{row.cost_micro_usd === null ? '—' : money(row.cost_micro_usd)}</td>
          <td className="ai-hide-sm">{row.billed_to === 'server' ? row.provider_label : m.yourKey}</td></tr>)}</tbody>
      </table>)}
    </section>
    <p className="help ai-help">{m.usageHelp} <HelpTip label={m.usageByFeature} text={m.usageWhy} /></p>
  </>;
}

const monthName = (value: string) => new Date(`${value}-01T00:00:00Z`).toLocaleDateString('en', { month: 'long', year: 'numeric', timeZone: 'UTC' });
