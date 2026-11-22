import { useEffect, useState } from 'react';
import { useCreateAction } from '../../api/use-create';
import { kbBulkApi } from './kb-bulk-api';
import type { KBBulkChange, KBBulkInput } from './kb-bulk-api';
import { kbBulkMessages as m } from './kb-bulk-messages';
import { reason } from './kb-tree';
import type { KBArticleFilters } from './knowledge-search';

export interface KBBulkToast { text: string; operation?: string }

// useKBBulk holds the rows the person ticked, or every article the filters match, and runs one action on
// them. A change of filters starts a new selection: the ticks of the old list name other rows.
export function useKBBulk(filter: KBArticleFilters, onChanged: () => Promise<unknown>) {
  const [ids, setIds] = useState<string[]>([]);
  const [all, setAll] = useState(false);
  const [toast, setToast] = useState<KBBulkToast>();
  const filterKey = JSON.stringify(filter);
  useEffect(() => { setIds([]); setAll(false); }, [filterKey]);
  const clear = () => { setIds([]); setAll(false); };
  // Unticking a row while every match is selected goes back to the rows on screen, less that one.
  const toggle = (id: string, shown: string[]) => {
    if (all) { setAll(false); setIds(shown.filter((other) => other !== id)); return; }
    setIds((current) => (current.includes(id) ? current.filter((other) => other !== id) : [...current, id]));
  };
  const apply = useCreateAction((input: KBBulkInput, key: string) => kbBulkApi.apply(input, key));
  const undo = useCreateAction((id: string, key: string) => kbBulkApi.undo(id, key));
  const run = async (change: KBBulkChange) => {
    // The filters as the list sends them: the server finds every match, on every page.
    const input: KBBulkInput = all ? { ...change, filter } : { ...change, ids };
    try {
      const result = await apply.run(input);
      clear();
      setToast({ text: m.done(result), operation: result.operation?.id });
    } catch (error) {
      setToast({ text: reason(error, m.failed) });
    }
    await onChanged();
  };
  const runUndo = async () => {
    if (!toast?.operation) return;
    try {
      setToast({ text: m.undone(await undo.run(toast.operation)) });
    } catch (error) {
      setToast({ text: reason(error, m.undoFailed) });
    }
    await onChanged();
  };
  return {
    ids, all, toggle, clear, selectAll: () => setAll(true), count: ids.length,
    run, undo: runUndo, toast, dismiss: () => setToast(undefined), busy: apply.pending || undo.pending,
  };
}
