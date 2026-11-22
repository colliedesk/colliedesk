import { useLayoutEffect, useState } from 'react';
import type { RefObject } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { kbListColumnSchema } from '../../api/schemas';
import type { KBListColumn, Me } from '../../api/schemas';
import { kbApi } from './kb-api';
import { ListMenu } from './list-menu';
import { listMessages } from './list-messages';

// The column widths of the article list, beside the title; a narrow list keeps only the first chosen column.
const widths: Record<KBListColumn, string> = {
  status: '7.5rem', collection: '9rem', author: '8rem', tags: '9rem', languages: '7rem', created: '8rem', updated: '8rem', votes: '5rem',
};

// fittingColumns counts how many of the chosen columns fit beside a title of at least 12rem in a list `width` px
// wide inside its scroll bar; the last chosen columns give way first, and the first column always stays. The row
// padding (with the tick box of bulk select) and the gaps between tracks are px tokens, the tracks are rem. A list
// box (`list`) of 36rem or less shows the title and the first column only (its container query in kb.css). A phone
// keeps every column: its rows show the status beside the title, whichever column comes first.
export function fittingColumns(columns: KBListColumn[], { width, list = width, phone = false, ordered, selects, rem }:
  { width: number; list?: number; phone?: boolean; ordered: boolean; selects: boolean; rem: number }): number {
  if (phone) return columns.length;
  const first = Math.min(1, columns.length);
  if (list <= 36 * rem) return first;
  const pad = 16 + (selects ? 16 + 12 : 0);
  const gap = 16;
  // The open button has a track of its own; in a collection's order the order buttons share it.
  const tail = (ordered ? 4.5 : 2.5) * rem;
  let used = pad + 12 * rem + tail + gap;
  let count = 0;
  for (const column of columns) {
    used += parseFloat(widths[column]) * rem + gap;
    if (used > width) break;
    count += 1;
  }
  return Math.max(count, first);
}

// The phone layout of kb.css starts below this width.
const desktop = '(min-width: 44.01rem)';

// atWidest says whether the page around the list is as wide as its content may grow (the max-width in kb.css):
// a wider window then gives the list no more room.
function atWidest(box: HTMLElement): boolean {
  const layout = box.closest<HTMLElement>('.kb-art-layout');
  if (!layout) return false;
  const max = parseFloat(getComputedStyle(layout).maxWidth);
  return Number.isFinite(max) && layout.getBoundingClientRect().width >= max - 1;
}

// useFittingColumns returns the chosen columns that fit the list box `scroll` (its width inside the scroll bar),
// following its size, and whether the page is at its widest. Until the box is measured every chosen column shows.
export function useFittingColumns(scroll: RefObject<HTMLElement | null>, columns: KBListColumn[], { ordered, selects }: { ordered: boolean; selects: boolean }):
  { shown: KBListColumn[]; widest: boolean } {
  const [size, setSize] = useState({ width: 0, list: 0, phone: false, widest: false });
  useLayoutEffect(() => {
    const box = scroll.current;
    if (!box) return;
    const measure = () => setSize({ width: box.clientWidth, list: box.closest('.kb-list')?.clientWidth ?? box.clientWidth,
      phone: typeof window.matchMedia === 'function' && !window.matchMedia(desktop).matches, widest: atWidest(box) });
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const watch = new ResizeObserver(measure);
    watch.observe(box);
    return () => watch.disconnect();
  }, [scroll]);
  if (size.width === 0) return { shown: columns, widest: size.widest };
  const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  return { shown: columns.slice(0, fittingColumns(columns, { ...size, ordered, selects, rem })), widest: size.widest };
}

// gridOf sets the list's grid for the chosen columns: the title, each column and the open-on-help-center
// button, which, while the list is its collection's order, shares its track with the order buttons.
export function gridOf(columns: KBListColumn[], ordered: boolean): React.CSSProperties {
  const tail = ordered ? '4.5rem' : '2.5rem';
  const first = columns[0];
  return {
    '--kb-grid': ['minmax(0, 1fr)', ...columns.map((column) => widths[column]), tail].join(' '),
    '--kb-span': columns.length + 1,
    '--kb-grid-narrow': ['minmax(0, 1fr)', ...(first ? [widths[first]] : []), tail].join(' '),
    '--kb-span-narrow': first ? 2 : 1,
  } as React.CSSProperties;
}

// ListColumns: the person picks the columns, saved for them on the server, so every device shows the same list.
// `hidden` columns did not fit; `widest` says a wider window would not make room for them.
export function ListColumns({ columns, isDefault, hidden = 0, widest = false }: { columns: KBListColumn[]; isDefault: boolean; hidden?: number; widest?: boolean }) {
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (next: KBListColumn[] | null) => kbApi.setListColumns(next),
    onSuccess: (answer) => client.setQueryData<Me>(['me'], (me) => me && { ...me, kb_list_columns: answer }),
  });
  const shown = save.isPending && save.variables ? save.variables : columns;
  const rest = kbListColumnSchema.options.filter((column) => !shown.includes(column));
  const toggle = (column: KBListColumn, on: boolean) => save.mutate(on ? [...shown, column] : shown.filter((item) => item !== column));
  return (
    <ListMenu label={listMessages.columns} className="kb-columns-menu">
      <p className="filter-hint">{listMessages.columnsLead}</p>
      {hidden > 0 && <p className="filter-hint" role="note">{widest ? listMessages.columnsNoRoom(hidden) : listMessages.columnsHidden(hidden)}</p>}
      {[...shown, ...rest].map((column) => (
        <label key={column} className="check-row"><input type="checkbox" checked={shown.includes(column)} disabled={save.isPending}
          onChange={(event) => toggle(column, event.currentTarget.checked)} /><span>{listMessages.columnNames[column]}</span></label>
      ))}
      {save.isError && <p role="alert" className="error-copy">{listMessages.columnsFailed}</p>}
      {!isDefault && <button type="button" className="link-action" disabled={save.isPending} onClick={() => save.mutate(null)}>{listMessages.resetColumns}</button>}
    </ListMenu>
  );
}
