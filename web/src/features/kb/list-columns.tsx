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

// gridOf sets the list's grid for the chosen columns: the title, each column, the open-on-help-center button
// and, while the list is its collection's order, the order buttons.
export function gridOf(columns: KBListColumn[], ordered: boolean): React.CSSProperties {
  const tail = `2.5rem${ordered ? ' 4.5rem' : ''}`;
  const first = columns[0];
  return {
    '--kb-grid': ['minmax(0, 1fr)', ...columns.map((column) => widths[column]), tail].join(' '),
    '--kb-span': columns.length + 1,
    '--kb-grid-narrow': ['minmax(0, 1fr)', ...(first ? [widths[first]] : []), tail].join(' '),
    '--kb-span-narrow': first ? 2 : 1,
  } as React.CSSProperties;
}

// ListColumns: the person picks the columns, saved for them on the server, so every device shows the same list.
export function ListColumns({ columns, isDefault }: { columns: KBListColumn[]; isDefault: boolean }) {
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
      {[...shown, ...rest].map((column) => (
        <label key={column} className="check-row"><input type="checkbox" checked={shown.includes(column)} disabled={save.isPending}
          onChange={(event) => toggle(column, event.currentTarget.checked)} /><span>{listMessages.columnNames[column]}</span></label>
      ))}
      {save.isError && <p role="alert" className="error-copy">{listMessages.columnsFailed}</p>}
      {!isDefault && <button type="button" className="link-action" disabled={save.isPending} onClick={() => save.mutate(null)}>{listMessages.resetColumns}</button>}
    </ListMenu>
  );
}
