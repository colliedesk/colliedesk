import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useEffect, useId, useState } from 'react';
import type { CSSProperties, KeyboardEvent, ReactNode } from 'react';
import { api } from '../../api/client';
import { apiQuery } from '../../lib/inbox-query';
import { messages } from '../../messages';
import { ChatIcon, SearchIcon, UserIcon } from '../../ui/icons';
import { contactApi } from '../contacts/contact-api';
import { contactAddress } from '../contacts/contact-messages';
import type { PaletteEntry, PaletteSource } from './palette-entries';
import { paletteMessages } from './palette-messages';
import { readRecent, recentHref } from './recent-items';
import './command-palette.css';

// One row of the list: it opens an address (href) or runs an action (run). group is its heading; '' has none.
interface Row { key: string; group: string; icon: ReactNode; label: string; detail?: string; kind?: string; href?: string; run?: () => void; marked?: boolean }

export interface CommandPaletteProps extends PaletteSource {
  workspaceId: string;
  // restoreFocus is false when a row was chosen: the page it opens (or the window it starts) takes the focus.
  onClose: (restoreFocus: boolean) => void;
  // Where the field that opened it sits: the palette centres on it, just above its top.
  anchor?: { center: number; top: number };
}

// A few of each are enough to pick one; See all results opens the whole list in the inbox.
const resultCount = 5;
// The search goes out once typing pauses, as the inbox's search box does.
const pauseMs = 150;

function useSettled(value: string) {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), pauseMs);
    return () => clearTimeout(timer);
  }, [value]);
  return settled;
}

// Marked sets each place the typed words appear in the name in bold.
function Marked({ text, words }: { text: string; words: string }) {
  if (!words) return text;
  const parts: ReactNode[] = [];
  const lower = text.toLowerCase();
  const needle = words.toLowerCase();
  let from = 0;
  for (let at = lower.indexOf(needle); at >= 0; at = lower.indexOf(needle, from)) {
    if (at > from) parts.push(text.slice(from, at));
    parts.push(<b key={at}>{text.slice(at, at + needle.length)}</b>);
    from = at + needle.length;
  }
  if (from < text.length) parts.push(text.slice(from));
  return parts;
}

const entryRow = (group: string, kind: string) => (entry: PaletteEntry): Row => ({ key: `${group}-${entry.key}`, group, icon: entry.icon, label: entry.label,
  detail: entry.detail, kind, href: entry.href, run: entry.run });

// CommandPalette searches the workspace from any page: with nothing typed it offers where the person was (Recent),
// where they can go and what they can start; typing lists matching conversations and customers from the same
// searches the inbox runs, and the last row opens the inbox list with those words. It is a combobox over a listbox:
// the focus stays in the field, the arrows move the chosen row (aria-activedescendant), Enter opens it.
export function CommandPalette({ workspaceId, places, actions, searchConversations, searchCustomers, onNavigate, onClose, anchor }: CommandPaletteProps) {
  const listId = useId();
  const [typed, setTyped] = useState('');
  const [active, setActive] = useState(0);
  const [recent] = useState(() => readRecent(workspaceId));
  const words = typed.trim();
  const settled = useSettled(words);
  // Earlier results stay until the next ones arrive, so the rows never jump while the person types.
  const conversations = useQuery({ queryKey: ['conversations', 'palette', settled],
    queryFn: ({ signal }) => api.conversations(`${apiQuery({ search: settled, status: 'all' }, 1, resultCount)}&counts=true`, signal),
    enabled: searchConversations && settled !== '', placeholderData: keepPreviousData });
  const customers = useQuery({ queryKey: ['contacts', 'search', settled, 'palette'], queryFn: ({ signal }) => contactApi.searchContacts(settled, resultCount, signal),
    enabled: searchCustomers && settled !== '', placeholderData: keepPreviousData });

  const rows: Row[] = [];
  if (!words) {
    rows.push(...recent.map((item): Row => ({ key: `recent-${item.kind}-${item.id}`, group: paletteMessages.recent, icon: item.kind === 'conversation' ? <ChatIcon /> : <UserIcon />,
      label: item.title, detail: item.detail, kind: item.kind === 'conversation' ? paletteMessages.conversation : paletteMessages.customer, href: recentHref(item) })));
    rows.push(...places.map(entryRow(paletteMessages.goTo, paletteMessages.page)), ...actions.map(entryRow(paletteMessages.actions, paletteMessages.action)));
  } else {
    if (searchConversations) {
      rows.push(...(conversations.data?.items ?? []).map((item): Row => ({ key: `conversation-${item.id}`, group: paletteMessages.conversations, icon: <ChatIcon />,
        label: item.subject, detail: `${item.contact_name} · ${messages.statuses[item.status]}`, kind: paletteMessages.conversation, href: `/conversations/${item.id}`, marked: true })));
    }
    if (searchCustomers) {
      rows.push(...(customers.data?.items ?? []).map((item): Row => ({ key: `customer-${item.id}`, group: paletteMessages.customers, icon: <UserIcon />, label: item.name,
        detail: [contactAddress(item), item.organization].filter(Boolean).join(' · '), kind: paletteMessages.customer, href: `/contacts/${item.id}`, marked: true })));
    }
    const named = (entry: PaletteEntry) => entry.label.toLowerCase().includes(words.toLowerCase());
    rows.push(...places.filter(named).map(entryRow(paletteMessages.goTo, paletteMessages.page)), ...actions.filter(named).map(entryRow(paletteMessages.actions, paletteMessages.action)));
    if (searchConversations) {
      // The count is of these words only: while the next search runs, the earlier one's count would mislead.
      const counts = settled === words && !conversations.isPlaceholderData ? conversations.data?.status_counts : undefined;
      const total = counts && counts.open + counts.pending + counts.resolved + counts.closed;
      rows.push({ key: 'see-all', group: '', icon: <SearchIcon />, label: paletteMessages.seeAll(words), detail: total === undefined ? undefined : paletteMessages.conversationCount(total),
        href: `/?search=${encodeURIComponent(words)}&status=all` });
    }
  }

  const current = rows.length === 0 ? -1 : Math.min(active, rows.length - 1);
  const optionId = (index: number) => `${listId}-${index}`;
  useEffect(() => {
    if (current >= 0) document.getElementById(optionId(current))?.scrollIntoView?.({ block: 'nearest' });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current]);

  const choose = (row: Row) => {
    onClose(false);
    if (row.run) row.run();
    else if (row.href) onNavigate(row.href);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
    // The field is the palette's only stop: Tab never walks out to the page behind it.
    if (event.key === 'Tab') { event.preventDefault(); return; }
    if (rows.length === 0) return;
    if (event.key === 'ArrowDown') { event.preventDefault(); setActive((current + 1) % rows.length); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActive((current - 1 + rows.length) % rows.length); }
    else if (event.key === 'Enter' && current >= 0) { event.preventDefault(); choose(rows[current]); }
  };

  // Consecutive rows of one heading form a group.
  const groups: Array<{ title: string; rows: Array<{ row: Row; index: number }> }> = [];
  rows.forEach((row, index) => {
    const last = groups[groups.length - 1];
    if (last && last.title === row.group) last.rows.push({ row, index });
    else groups.push({ title: row.group, rows: [{ row, index }] });
  });
  const position = anchor ? { '--palette-center': `${anchor.center}px`, '--palette-top': `${anchor.top}px` } as CSSProperties : undefined;
  return (
    <div className="palette-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) { event.preventDefault(); onClose(true); } }}>
      <div className="palette" role="dialog" aria-modal="true" aria-label={paletteMessages.dialog} style={position}>
        <div className="palette-head">
          <SearchIcon />
          <input type="text" role="combobox" aria-expanded="true" aria-controls={listId} aria-autocomplete="list" autoFocus autoComplete="off" spellCheck={false}
            aria-label={paletteMessages.placeholder} placeholder={paletteMessages.placeholder} value={typed}
            aria-activedescendant={current >= 0 ? optionId(current) : undefined}
            onChange={(event) => { setTyped(event.target.value); setActive(0); }} onKeyDown={onKeyDown} />
          <kbd aria-hidden="true">Esc</kbd>
        </div>
        <div className="palette-list" role="listbox" id={listId} aria-label={paletteMessages.results}>
          {groups.map((group) => <div key={`${group.title}-${group.rows[0].index}`} role="group" className="palette-group" aria-label={group.title || undefined}>
            {group.title ? <div className="palette-group-title" aria-hidden="true">{group.title}</div> : <div className="palette-group-gap" />}
            {group.rows.map(({ row, index }) => {
              const chosen = index === current;
              return <div key={row.key} id={optionId(index)} role="option" aria-selected={chosen} className="palette-row"
                onMouseMove={() => { if (!chosen) setActive(index); }} onMouseDown={(event) => event.preventDefault()} onClick={() => choose(row)}>
                {row.icon}
                <span className="palette-text"><span className="palette-name">{row.marked ? <Marked text={row.label} words={words} /> : row.label}</span>
                  {row.detail && <span className="palette-detail">{row.detail}</span>}</span>
                <span className="palette-kind">{chosen && <kbd aria-hidden="true">↵</kbd>}
                  {row.kind && <span className={chosen ? 'sr-only' : undefined}>{row.kind}</span>}</span>
              </div>;
            })}
          </div>)}
          {words && rows.length === 0 && <p className="palette-empty">{paletteMessages.nothingFound}</p>}
        </div>
        <div className="palette-foot">
          <span><kbd>↑↓</kbd>{paletteMessages.move}</span>
          <span><kbd>↵</kbd>{paletteMessages.open}</span>
          <span><kbd>Esc</kbd>{paletteMessages.close}</span>
        </div>
      </div>
    </div>
  );
}
