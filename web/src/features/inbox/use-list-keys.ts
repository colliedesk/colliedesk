import { useEffect, useRef } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, RefObject } from 'react';
import { composing, isTyping } from '../../lib/typing-target';

const letters: Record<string, number> = { j: 1, k: -1 };
const arrows: Record<string, number> = { ArrowDown: 1, ArrowUp: -1 };
const held = (event: { ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }) => event.ctrlKey || event.metaKey || event.altKey || event.shiftKey;

// useListKeys moves through the conversation list from the keyboard, as Gmail and Help Scout do: j and k
// anywhere on the page outside a text field, ArrowDown and ArrowUp while focus is on a row or its box. A
// move opens the next or previous conversation and puts the focus on its row. It counts from the row with
// the focus, else the open one (or the place it left); with neither, j opens the first row. The ends of the page stay put.
// Returns the key handler for the list itself.
export function useListKeys(list: RefObject<HTMLElement | null>, ids: string[], selectedId: string | undefined, onSelect?: (id: string) => void) {
  const latest = useRef({ ids, selectedId, onSelect });
  // The rows around the open conversation when it was last in the list. Once it leaves (resolved, reassigned) j opens
  // the row that was below it and k the one above, instead of starting again at the top. Ids, not a row number: a
  // refresh may add rows on top in the same answer, and another view or page holds neither neighbour, so j starts at its top.
  const openAt = useRef<{ id: string; above?: string; below?: string }>(undefined);
  useEffect(() => {
    latest.current = { ids, selectedId, onSelect };
    const index = selectedId ? ids.indexOf(selectedId) : -1;
    if (selectedId && index !== -1) openAt.current = { id: selectedId, above: ids[index - 1], below: ids[index + 1] };
  }, [ids, selectedId, onSelect]);
  // Where j (step 1) or k (step -1) goes from an open conversation that is no longer in the list: its old neighbour on
  // that side, else the row next to the neighbour on the other side; undefined when neither is still there.
  const fromLeft = (order: string[], open: string, step: number): number | undefined => {
    const around = openAt.current;
    if (around?.id !== open) return undefined;
    const [near, far] = step > 0 ? [around.below, around.above] : [around.above, around.below];
    const nearAt = near ? order.indexOf(near) : -1;
    if (nearAt !== -1) return nearAt;
    const farAt = far ? order.indexOf(far) : -1;
    return farAt !== -1 ? farAt + step : undefined;
  };
  const move = (step: number, target: EventTarget | null): boolean => {
    const { ids: order, selectedId: open, onSelect: select } = latest.current;
    if (!select || !list.current) return false;
    const focused = target instanceof Element ? target.closest<HTMLElement>('[data-conversation-id]')?.dataset.conversationId : undefined;
    const from = order.indexOf(focused ?? open ?? '');
    const left = from === -1 && !focused && open ? fromLeft(order, open, step) : undefined;
    const next = from !== -1 ? from + step : left ?? (step > 0 ? 0 : -1);
    if (next < 0 || next >= order.length) return false;
    select(order[next]);
    const row = list.current.querySelector<HTMLElement>(`[data-conversation-id="${order[next]}"] .conversation-row`);
    row?.focus();
    row?.scrollIntoView?.({ block: 'nearest' });
    return true;
  };
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const step = letters[event.key];
      if (!step || event.defaultPrevented || composing(event) || held(event) || isTyping(event.target)) return;
      // A dialog over the inbox keeps its own keys.
      if (event.target instanceof Element && event.target.closest('[role="dialog"], [role="alertdialog"]')) return;
      if (move(step, event.target)) event.preventDefault();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
    // move reads everything it needs from refs, so the listener is added once.
  }, []);
  return (event: ReactKeyboardEvent<HTMLElement>) => {
    const step = arrows[event.key];
    if (!step || held(event) || composing(event.nativeEvent)) return;
    if (!(event.target instanceof Element) || !event.target.closest('.conversation-item')) return;
    if (move(step, event.target)) event.preventDefault();
  };
}
