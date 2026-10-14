import type { TimeEntry } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { formatMinutes } from '../../lib/format-minutes';
import { messages } from '../../messages';

export function TimeEntriesSection({ items, timezone }: { items: TimeEntry[]; timezone: string }) {
  const total = items.reduce((sum, item) => sum + item.minutes, 0);
  return (
    <section className="time-entries" aria-label={messages.timeLogged}>
      <h3>{messages.timeLogged}</h3>
      <p>{messages.totalTime(formatMinutes(total))}</p>
      <ul>
        {items.map((item) => (
          <li key={item.id}>
            <span>{`${item.agent_name || '—'} · ${formatMinutes(item.minutes)}`}</span>
            <span>{item.billable ? messages.billable : messages.notBillable}</span>
            <time dateTime={item.executed_at}>{formatDate(item.executed_at, timezone)}</time>
            {item.note && <span className="time-entry-note">{item.note}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}
