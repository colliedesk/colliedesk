import { useEffect, useRef } from 'react';
import type { Conversation, Tag, TeamMember } from '../../api/schemas';
import { messages } from '../../messages';
import { FilterIcon, XIcon } from '../../ui/icons';
import { zonedDay, zonedDayStart } from '../../lib/zoned-day';
import { listOf, withoutValue } from '../../lib/inbox-query-edit';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';
import { DraftInput } from './draft-input';
import { trackFilterChanges } from './filter-analytics';
import { filterMessages as text } from './filter-messages';

// The Filter menu, the Sort menu and the chips of the active filters. They load on their own chunk, right
// after the first screen (inbox-filters.tsx).

export interface FilterPartProps {
  query: InboxQuery;
  onChange?: (query: InboxQuery) => void;
  tags: Tag[];
  agents: TeamMember[];
  timezone: string;
  conversations?: Conversation[];
}

const priorities = ['urgent', 'high', 'normal', 'low'] as const;
const channels = Object.keys(text.channels) as (keyof typeof text.channels)[];
const dateFilters = [['created', 'created_after', 'created_before'], ['updated', 'updated_after', 'updated_before']] as const;

// The day before an exclusive end is the last day the person picked.
function lastDay(before: string, timeZone: string): string {
  return zonedDay(new Date(new Date(before).getTime() - 1).toISOString(), timeZone);
}

function nextDayStart(day: string, timeZone: string): string | undefined {
  const [year, month, date] = day.split('-').map(Number);
  const next = new Date(Date.UTC(year, month - 1, date + 1)).toISOString().slice(0, 10);
  return zonedDayStart(next, timeZone);
}

// A whole day typed into a date field; a year still being typed (0002) is not one.
const wholeDay = (value: string) => value === '' || /^[1-9]\d{3}-\d{2}-\d{2}$/.test(value);

// Filters counted on the Filter button; status, sort and search have their own controls, and a date range
// counts once.
const menuKeys: InboxQueryKey[] = ['priority', 'assignee', 'tag', 'contact', 'organization', 'awaiting', 'has_pending_draft', 'delivery_failed',
  'source', 'channel', 'created_after', 'updated_after'];
const has = (query: InboxQuery, key: InboxQueryKey) => query[key] !== undefined && query[key] !== '';

// changeOf replaces the filters (reporting what kind changed) and sets one of them.
function changeOf({ query, onChange }: FilterPartProps) {
  const change = (next: InboxQuery) => { trackFilterChanges(query, next); onChange?.(next); };
  const set = (key: InboxQueryKey, value: string | boolean | undefined) => {
    const next: InboxQuery = { ...query };
    if (value === undefined || value === '') delete next[key];
    else (next as Record<string, string | boolean>)[key] = value;
    if (!next.tag) delete next.tag_match;
    change(next);
  };
  return { change, set };
}

export function FilterBar(props: FilterPartProps) {
  const { query } = props;
  const { set } = changeOf(props);
  const menu = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const close = (event: PointerEvent) => { if (menu.current?.open && !menu.current.contains(event.target as Node)) menu.current.open = false; };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, []);
  // Keyboard users leave the menu with Tab; it closes like it does on an outside click.
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  const active = menuKeys.filter((key) => has(query, key)).length
    + (!has(query, 'created_after') && has(query, 'created_before') ? 1 : 0) + (!has(query, 'updated_after') && has(query, 'updated_before') ? 1 : 0);
  return (
    <>
      <details className="filter-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut}>
        <summary><FilterIcon /><span>{messages.filter}</span>
          {active > 0 && <span className="filter-count" aria-label={messages.filtersActive(active)}>{active}</span>}</summary>
        <div className="filter-popover" role="group" aria-label={messages.filter}><FilterPanel {...props} /></div>
      </details>
      <select className="sort-field" aria-label={messages.inboxSort} value={query.sort ?? 'updated_desc'} onChange={(event) => set('sort', event.currentTarget.value === 'updated_desc' ? undefined : event.currentTarget.value)}>
        {Object.entries(text.sorts).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </>
  );
}

function FilterPanel(props: FilterPartProps) {
  const { query, tags, agents, timezone } = props;
  const { set } = changeOf(props);
  const toggle = (key: 'priority' | 'assignee' | 'tag' | 'channel', value: string, on: boolean) => {
    const values = listOf(query, key).filter((item) => item !== value);
    if (on) values.push(value);
    set(key, values.join(','));
  };
  const onDate = (key: InboxQueryKey, day: string, end: boolean) => set(key, day ? (end ? nextDayStart(day, timezone) : zonedDayStart(day, timezone)) : undefined);
  const awaiting = query.awaiting ?? '';
  const source = query.source ?? '';
  const chosenTags = listOf(query, 'tag');
  const check = (key: 'priority' | 'assignee' | 'tag' | 'channel', value: string, label: string) => (
    <label key={value} className="check-row"><input type="checkbox" checked={listOf(query, key).includes(value)} onChange={(event) => toggle(key, value, event.currentTarget.checked)} /><span>{label}</span></label>
  );
  return (
    <>
      <fieldset><legend>{text.priority}</legend>{priorities.map((value) => check('priority', value, messages.priorities[value]))}</fieldset>
      <fieldset><legend>{text.assignee}</legend>
        {check('assignee', 'me', messages.assignedToMe)}{check('assignee', 'unassigned', messages.unassigned)}{check('assignee', 'anyone', text.anyoneAssigned)}
        {agents.filter((agent) => !agent.deactivated_at).map((agent) => check('assignee', agent.id, agent.display_name || agent.email))}
      </fieldset>
      <fieldset><legend>{text.tags}</legend>
        {tags.length === 0 ? <p className="filter-hint">{text.noTags}</p> : tags.map((tag) => check('tag', tag.id, tag.name))}
        {tags.length > 1 && <label className="check-row filter-match"><input type="checkbox" checked={query.tag_match === 'all'} disabled={chosenTags.length < 2 && query.tag_match !== 'all'}
          onChange={(event) => set('tag_match', event.currentTarget.checked ? 'all' : undefined)} /><span>{text.matchAllTags}</span></label>}
      </fieldset>
      <fieldset><legend>{text.awaiting}</legend>
        {(['', 'team', 'customer'] as const).map((value) => (
          <label key={value || 'any'} className="check-row"><input type="radio" name="awaiting" checked={awaiting === value} onChange={() => set('awaiting', value || undefined)} />
            <span>{value ? text.awaitingOptions[value] : text.awaitingAny}</span></label>))}
      </fieldset>
      <fieldset><legend>{text.attention}</legend>
        <label className="check-row"><input type="checkbox" checked={query.has_pending_draft === true} onChange={(event) => set('has_pending_draft', event.currentTarget.checked || undefined)} /><span>{text.hasPendingDraft}</span></label>
        <label className="check-row"><input type="checkbox" checked={query.delivery_failed === true} onChange={(event) => set('delivery_failed', event.currentTarget.checked || undefined)} /><span>{text.deliveryFailed}</span></label>
      </fieldset>
      <fieldset><legend>{text.channel}</legend>{channels.map((value) => check('channel', value, text.channels[value]))}</fieldset>
      <fieldset><legend>{text.source}</legend>
        {(['', 'imported', 'native'] as const).map((value) => (
          <label key={value || 'any'} className="check-row"><input type="radio" name="source" checked={source === value} onChange={() => set('source', value || undefined)} />
            <span>{value ? text.sources[value] : text.sourceAny}</span></label>))}
      </fieldset>
      <label className="field"><span>{text.organization}</span>
        <DraftInput type="text" value={query.organization ?? ''} maxLength={200} placeholder={text.organizationHint} delay={100} onCommit={(value) => set('organization', value)} /></label>
      {dateFilters.map(([name, after, before]) => (
        <fieldset key={name} className="date-range" aria-label={text[name]}><legend>{text[name]}</legend>
          {/* From and To bound each other, so a range never ends before it starts. */}
          <label className="field"><span>{text.from}</span><DraftInput type="date" value={query[after] ? zonedDay(query[after], timezone) : ''} max={query[before] ? lastDay(query[before], timezone) : undefined}
            ready={wholeDay} onCommit={(day) => onDate(after, day, false)} /></label>
          <label className="field"><span>{text.to}</span><DraftInput type="date" value={query[before] ? lastDay(query[before], timezone) : ''} min={query[after] ? zonedDay(query[after], timezone) : undefined}
            ready={wholeDay} onCommit={(day) => onDate(before, day, true)} /></label>
        </fieldset>))}
    </>
  );
}

interface Chip {
  key: InboxQueryKey;
  value?: string;
  label: string;
}

export function FilterChips(props: FilterPartProps) {
  const { query, tags, agents, timezone, conversations } = props;
  const { change: onChange } = changeOf(props);
  const agentName = (id: string) => {
    const agent = agents.find((item) => item.id === id);
    return agent ? agent.display_name || agent.email : text.unknownAgent;
  };
  const assigneeLabel = (value: string) => (value === 'me' ? messages.assignedToMe : value === 'unassigned' ? messages.unassigned : value === 'anyone' ? text.anyoneAssigned : agentName(value));
  const tagName = (id: string) => tags.find((tag) => tag.id === id)?.name ?? text.unknownTag;
  // In the order of the Filter menu.
  const chips: Chip[] = [
    ...listOf(query, 'priority').map((value) => ({ key: 'priority' as const, value, label: `${text.priority}: ${messages.priorities[value as keyof typeof messages.priorities] ?? value}` })),
    ...listOf(query, 'assignee').map((value) => ({ key: 'assignee' as const, value, label: `${text.assignee}: ${assigneeLabel(value)}` })),
    ...listOf(query, 'tag').map((value) => ({ key: 'tag' as const, value, label: `${text.tags}: ${tagName(value)}` })),
    ...(query.contact ? [{ key: 'contact' as const, label: `${text.contact}: ${conversations?.find((item) => item.contact_id === query.contact)?.contact_name ?? text.selectedContact}` }] : []),
    ...(query.awaiting === 'team' || query.awaiting === 'customer' ? [{ key: 'awaiting' as const, label: `${text.awaiting}: ${text.awaitingOptions[query.awaiting]}` }] : []),
    ...(query.has_pending_draft !== undefined ? [{ key: 'has_pending_draft' as const, label: query.has_pending_draft ? text.hasPendingDraft : text.noPendingDraft }] : []),
    ...(query.delivery_failed !== undefined ? [{ key: 'delivery_failed' as const, label: query.delivery_failed ? text.deliveryFailed : text.noDeliveryFailed }] : []),
    ...listOf(query, 'channel').map((value) => ({ key: 'channel' as const, value, label: `${text.channel}: ${text.channels[value as keyof typeof text.channels] ?? value}` })),
    ...(query.source === 'imported' || query.source === 'native' ? [{ key: 'source' as const, label: `${text.source}: ${text.sources[query.source]}` }] : []),
    ...(query.organization ? [{ key: 'organization' as const, label: `${text.organization}: ${query.organization}` }] : []),
    ...dateFilters.flatMap(([name, after, before]) => (query[after] || query[before]
      ? [{ key: after, label: `${text[name]}: ${text.dateRange(query[after] ? zonedDay(query[after], timezone) : undefined, query[before] ? lastDay(query[before], timezone) : undefined)}` }]
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
        {chips.map((chip) => (
          <li key={`${chip.key}:${chip.value ?? ''}`}>{chip.label}
            <button type="button" className="chip-remove" aria-label={text.remove(chip.label)} onClick={() => removeChip(chip)}><XIcon /></button></li>))}
      </ul>
      <button type="button" className="link-button" onClick={() => onChange?.({})}>{text.clearAll}</button>
    </div>
  );
}
