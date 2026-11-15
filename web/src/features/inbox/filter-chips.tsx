import { useContext, useEffect, useState } from 'react';
import { QueryClientContext } from '@tanstack/react-query';
import { resultsApi } from '../../api/results';
import type { Results } from '../../api/results';
import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';
import { zonedDay } from '../../lib/zoned-day';
import { listOf, withoutValue } from '../../lib/inbox-query-edit';
import type { InboxQueryKey } from '../../lib/inbox-query';
import { channelLabel, filterMessages as text } from './filter-messages';
import { changeOf, dateFilters, useChannelList } from './filter-part';
import type { FilterPartProps } from './filter-part';

// The chips of the active filters, one per value, each removing it, then Clear. They load with the Filter menu's chunk.

// The day before an exclusive end is the last day the person picked.
function lastDay(before: string, timeZone: string): string {
  return zonedDay(new Date(new Date(before).getTime() - 1).toISOString(), timeZone);
}

// Metrics counted on the conversations created a week earlier (docs/user/results.md).
const resolvedMetrics = new Set(['resolution', 'unresolved', 'without_resolve_time', 'reopened', 'handle']);

// A number of the results page: `<snapshot id>:<window>.<metric>`. With the page's numbers at hand (they
// are cached from Results, or read once) the chip says which side it counts and the days it covers.
export function resultLabel(result: string, results: Results | undefined, timeZone: string): string {
  const [snapshot, key] = result.split(':');
  const [window, metric] = (key ?? '').split('.');
  const metricName = text.resultMetrics[metric] ?? metric;
  const side = results?.current?.id === snapshot ? results.current : results?.baseline?.id === snapshot ? results.baseline : undefined;
  const numbers = side?.windows[window as '30d' | '7d'];
  if (!side || !numbers) return text.countedIn(metricName, text.resultWindows[window] ?? window);
  const name = side === results?.current ? 'Colliedesk' : side.source?.startsWith('helpscout:') ? 'Help Scout' : 'Freshdesk';
  const span = resolvedMetrics.has(metric) ? numbers.resolved_created : numbers.created;
  const day = (value: string) => zonedDayLabel(value, timeZone);
  return text.countedInSide(metricName, name, `${day(span.from)} – ${day(span.to)}`);
}

function zonedDayLabel(value: string, timeZone: string): string {
  try {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric', timeZone }).format(new Date(value));
  } catch {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' }).format(new Date(value));
  }
}

interface Chip {
  key: InboxQueryKey;
  value?: string;
  // field names the filter ("Priority"); a chip that is a whole sentence ("Waiting on us") has none.
  field?: string;
  text: string;
}

export function FilterChips(props: FilterPartProps) {
  const { query, tags, agents, timezone, conversations, scopeChannel } = props;
  const { change: onChange } = changeOf(props);
  // The numbers of the results page name the side and days of a result chip: cached from Results, or
  // read once for a link opened directly.
  const cached = useContext(QueryClientContext)?.getQueryData<Results>(['results']);
  const [fetched, setFetched] = useState<Results>();
  useEffect(() => {
    if (!query.result || cached) return undefined;
    let live = true;
    resultsApi.get().then((value) => { if (live) setFetched(value); }, () => {});
    return () => { live = false; };
  }, [query.result, cached]);
  const results = cached ?? fetched;
  // The channel the list's title names has no chip: the title already says it.
  const chipChannels = listOf(query, 'channel_id').filter((id) => id !== scopeChannel);
  const channelList = useChannelList(chipChannels.length > 0);
  const channelName = (id: string) => {
    const channel = channelList?.items.find((item) => item.id === id);
    return !channel ? messages.unknownChannel : channel.deleted_at ? text.removedChannel(channel.name) : channel.name;
  };
  const agentName = (id: string) => {
    const agent = agents.find((item) => item.id === id);
    return agent ? agent.display_name || agent.email : text.unknownAgent;
  };
  const assigneeLabel = (value: string) => (value === 'me' ? messages.assignToMe : value === 'unassigned' ? messages.unassigned : value === 'anyone' ? text.anyoneAssigned : agentName(value));
  const tagName = (id: string) => tags.find((tag) => tag.id === id)?.name ?? text.unknownTag;
  // In the order of the Filter menu.
  const chips: Chip[] = [
    ...listOf(query, 'priority').map((value) => ({ key: 'priority' as const, value, field: text.priority, text: messages.priorities[value as keyof typeof messages.priorities] ?? value })),
    ...listOf(query, 'assignee').map((value) => ({ key: 'assignee' as const, value, field: text.assignee, text: assigneeLabel(value) })),
    ...listOf(query, 'tag').map((value) => ({ key: 'tag' as const, value, field: text.tag, text: tagName(value) })),
    ...(query.contact ? [{ key: 'contact' as const, field: text.contact, text: conversations?.find((item) => item.contact_id === query.contact)?.contact_name ?? text.selectedContact }] : []),
    ...(query.awaiting === 'team' || query.awaiting === 'customer' ? [{ key: 'awaiting' as const, text: text.awaitingOptions[query.awaiting] }] : []),
    ...(query.has_pending_draft !== undefined ? [{ key: 'has_pending_draft' as const, text: query.has_pending_draft ? text.hasPendingDraft : text.noPendingDraft }] : []),
    ...(query.delivery_failed !== undefined ? [{ key: 'delivery_failed' as const, text: query.delivery_failed ? text.deliveryFailed : text.noDeliveryFailed }] : []),
    ...listOf(query, 'sla').map((value) => ({ key: 'sla' as const, value, field: text.targets, text: text.targetStates[value as 'overdue' | 'due_soon'] ?? value })),
    ...listOf(query, 'channel').map((value) => ({ key: 'channel' as const, value, field: text.channel, text: channelLabel(value) })),
    ...chipChannels.map((value) => ({ key: 'channel_id' as const, value, field: text.channel, text: channelName(value) })),
    ...(query.source === 'imported' || query.source === 'native' ? [{ key: 'source' as const, field: text.source, text: text.sources[query.source] }] : []),
    ...(query.organization ? [{ key: 'organization' as const, field: text.organization, text: query.organization }] : []),
    ...(query.ref ? [{ key: 'ref' as const, field: text.reference, text: query.ref }] : []),
    ...(query.result ? [{ key: 'result' as const, text: resultLabel(query.result, results, timezone) }] : []),
    ...dateFilters.flatMap(([name, after, before]) => (query[after] || query[before]
      ? [{ key: after, field: text[name], text: text.dateRange(query[after] ? zonedDay(query[after], timezone) : undefined, query[before] ? lastDay(query[before], timezone) : undefined) }]
      : [])),
  ];
  if (chips.length === 0) return null;
  const removeChip = (chip: Chip) => {
    let next = withoutValue(query, chip.key, chip.value);
    // A date chip stands for both ends of its range.
    for (const [, after, before] of dateFilters) if (chip.key === after) next = withoutValue(next, before);
    onChange?.(next);
  };
  return (
    <div className="filter-chips">
      <ul aria-label={text.active}>
        {chips.map((chip) => {
          const label = chip.field ? `${chip.field}: ${chip.text}` : chip.text;
          // The field reads quieter than its value; a screen reader hears "Priority: Urgent".
          return <li key={`${chip.key}:${chip.value ?? ''}`}>
            {chip.field && <span className="chip-field">{chip.field}<span className="sr-only">: </span></span>}<span className="chip-value">{chip.text}</span>
            <button type="button" className="chip-remove" aria-label={text.remove(label)} onClick={() => removeChip(chip)}><XIcon /></button></li>;
        })}
      </ul>
      {/* Clear goes back to Open in the same place: the channel the title names is where the list is, not a filter. */}
      <button type="button" className="link-button chips-clear" onClick={() => onChange?.(scopeChannel ? { channel_id: scopeChannel } : {})}>{text.clearAll}</button>
      {query.result && <a href={query.result.includes(':7d.') ? '/results?window=7d' : '/results'} className="link-button">{text.backToResults}</a>}
      {query.result && <p className="filter-chips-note">{text.anyStatus}</p>}
    </div>
  );
}
