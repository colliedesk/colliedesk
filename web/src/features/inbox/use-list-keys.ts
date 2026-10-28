import { useEffect, useRef } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, RefObject } from 'react';
import { composing, isTyping } from '../../lib/typing-target';

const letters: Record<string, number> = { j: 1, k: -1 };
const arrows: Record<string, number> = { ArrowDown: 1, ArrowUp: -1 };
const held = (event: { ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }) => event.ctrlKey || event.metaKey || event.altKey || event.shiftKey;

// useListKeys moves through the conversation list from the keyboard, as Gmail and Help Scout do: j and k
// anywhere on the page outside a text field, ArrowDown and ArrowUp while focus is on a row or its box. A
// move opens the next or previous conversation and puts the focus on its row. It counts from the row with
// the focus, else the open one; with neither, j opens the first row. The ends of the page stay put.
// Returns the key handler for the list itself.
export function useListKeys(list: RefObject<HTMLElement | null>, ids: string[], selectedId: string | undefined, onSelect?: (id: string) => void) {
  const latest = useRef({ ids, selectedId, onSelect });
  useEffect(() => { latest.current = { ids, selectedId, onSelect }; }, [ids, selectedId, onSelect]);
  const move = (step: number, target: EventTarget | null): boolean => {
    const { ids: order, selectedId: open, onSelect: select } = latest.current;
    if (!select || !list.current) return false;
    const focused = target instanceof Element ? target.closest<HTMLElement>('[data-conversation-id]')?.dataset.conversationId : undefined;
    const from = order.indexOf(focused ?? open ?? '');
    const next = from === -1 ? (step > 0 ? 0 : -1) : from + step;
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
