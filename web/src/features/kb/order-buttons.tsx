import { useLayoutEffect, useRef, useState } from 'react';
import type { MutableRefObject } from 'react';
import { ConflictError } from '../../api/client';
import { Icon } from './editor/icons';
import { kbApi } from './kb-api';
import { kbMessages } from './kb-messages';

export type Step = -1 | 1;
export type OrderLists = { collection_ids: string[]; article_ids: string[] };

// moveInOrder moves id one place up (-1) or down (1); at an end, or when id is not there, nothing moves.
export function moveInOrder(ids: string[], id: string, step: Step): string[] {
  const from = ids.indexOf(id);
  const to = from + step;
  if (from < 0 || to < 0 || to >= ids.length) return ids;
  const next = [...ids];
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}

// staleLists are the lists an order_stale refusal holds: what the parent holds now.
function staleLists(error: unknown): OrderLists | undefined {
  if (!(error instanceof ConflictError) || error.detail?.code !== 'order_stale') return undefined;
  const { collection_ids: collections, article_ids: articles } = error.detail;
  const strings = (value: unknown) => Array.isArray(value) && value.every((item) => typeof item === 'string');
  return strings(collections) && strings(articles) ? { collection_ids: collections as string[], article_ids: articles as string[] } : undefined;
}

// OrderGoneError: the row to move is no longer under the parent (someone else moved or deleted it), so
// there is nothing to send.
export class OrderGoneError extends Error {
  constructor() {
    super('The row is no longer in this list.');
    this.name = 'OrderGoneError';
  }
}

// allArticleIds reads the ids of every article of a collection, first to last, at the largest page.
export async function allArticleIds(collection: string): Promise<string[]> {
  const found: string[] = [];
  let cursor: string | undefined;
  do {
    const page = await kbApi.articles({ collection_id: collection, sort: 'manual', limit: 200, cursor });
    found.push(...page.items.map((item) => item.id));
    cursor = page.next_cursor ?? undefined;
  } while (cursor);
  return found;
}

// sendMove moves id one place in its list (kind) and sends the order of everything under parent. The
// page reads the lists in full before it calls; if they went stale since, the server answers with the
// lists it has, and the move is made in those and sent once more. A row that is not in the lists is an
// OrderGoneError and nothing is sent. It returns the list of kind as it was saved.
export async function sendMove(parent: string | null, lists: OrderLists, kind: keyof OrderLists, id: string, step: Step): Promise<string[]> {
  const send = async (from: OrderLists) => {
    if (!from[kind].includes(id)) throw new OrderGoneError();
    const order = { ...from, [kind]: moveInOrder(from[kind], id, step) };
    await kbApi.setOrder({ parent_id: parent, ...order });
    return order[kind];
  };
  try {
    return await send(lists);
  } catch (error) {
    const current = staleLists(error);
    if (!current) throw error;
    return send(current);
  }
}

interface OrderButtonsProps {
  // ids are the row and its siblings as the page lists them, first to last.
  ids: string[];
  id: string;
  // name is how the row is read out: in the buttons' names and in the announcement.
  name: string;
  // more says rows follow that the page has not loaded, so the last one listed can still move down.
  more?: boolean;
  // busy holds the buttons while another row's move is sent.
  busy?: boolean;
  // text shows the buttons' names beside the arrows, where they stand among other named buttons.
  text?: boolean;
  // onMove saves next (the order after this move) and resolves with the order saved, when it knows it.
  onMove: (next: string[], step: Step) => Promise<string[] | void>;
  onAnnounce: (message: string) => void;
  // focus is the row to focus once the list shows it in its new place (useOrderMoves). Shared by the rows
  // of a list, it outlives a row that leaves the loaded page and comes back on the next one.
  focus?: MutableRefObject<PendingFocus | null>;
}

export type PendingFocus = { id: string; step: Step };

// OrderButtons move a row one place up or down. They are plain buttons, so the keyboard reaches them;
// the result is read out through OrderAnnouncement, and the focus stays on the row that moved.
export function OrderButtons({ ids, id, name, more = false, busy = false, text = false, onMove, onAnnounce, focus }: OrderButtonsProps) {
  const place = ids.indexOf(id);
  const up = useRef<HTMLButtonElement>(null);
  const down = useRef<HTMLButtonElement>(null);
  const [moving, setMoving] = useState(false);
  // The row and direction last clicked, until the list shows the row in its new place.
  const own = useRef<PendingFocus | null>(null);
  const pending = focus ?? own;
  // The page builds ids anew on every render; only a new order, or the row coming back, moves the focus,
  // in the same commit that shows the row, before anything reads where the focus is.
  const order = ids.join(' ');
  useLayoutEffect(() => {
    const target = pending.current;
    if (target?.id !== id) return;
    pending.current = null;
    (target.step === -1 ? up.current ?? down.current : down.current ?? up.current)?.focus();
  }, [order, id, pending]);
  const move = async (step: Step) => {
    if (moving || busy) return;
    setMoving(true);
    pending.current = { id, step };
    try {
      const saved = await onMove(moveInOrder(ids, id, step), step) ?? moveInOrder(ids, id, step);
      onAnnounce(kbMessages.moved(name, saved.indexOf(id) + 1, saved.length));
    } catch (error) {
      if (pending.current?.id === id) pending.current = null;
      onAnnounce(error instanceof OrderGoneError ? kbMessages.moveGone(name) : kbMessages.moveFailed(name));
    } finally {
      setMoving(false);
    }
  };
  const held = moving || busy;
  // In a list a missing button keeps its place, so the arrows of every row line up.
  const gap = text ? null : <span className="kb-order-gap" aria-hidden="true" />;
  const button = (step: Step) => (
    <button ref={step === -1 ? up : down} type="button" className={text ? undefined : 'icon-button'} aria-disabled={held || undefined}
      aria-label={step === -1 ? kbMessages.moveNameUp(name) : kbMessages.moveNameDown(name)} data-tip={text ? undefined : step === -1 ? kbMessages.moveUp : kbMessages.moveDown}
      onClick={() => void move(step)}>
      <Icon name={step === -1 ? 'chevronUp' : 'chevronDown'} />{text && (step === -1 ? kbMessages.moveUp : kbMessages.moveDown)}
    </button>
  );
  return (
    <span className="kb-order" data-text={text || undefined}>
      {place > 0 ? button(-1) : gap}
      {place < ids.length - 1 || more ? button(1) : gap}
    </span>
  );
}

// useOrderMoves sends one move at a time, reads the lists again after it (also when the row turned out
// to be gone, so the page shows where things are now), keeps what the last move should read out
// (OrderAnnouncement) and the row the focus goes back to (OrderButtons focus); the move's own catch in
// OrderButtons reads out why it failed.
// landing moves the focus somewhere sensible when the moved row cannot be shown.
export function useOrderMoves(refresh: () => Promise<unknown>, landing: () => void) {
  const [message, setMessage] = useState('');
  const [moving, setMoving] = useState(false);
  const focus = useRef<PendingFocus | null>(null);
  // lists may resolve the lists to send (reading what the page has not loaded) inside the same hold.
  const send = async (parent: string | null, lists: OrderLists | (() => Promise<OrderLists>), kind: keyof OrderLists, id: string, step: Step) => {
    setMoving(true);
    try {
      const saved = await sendMove(parent, typeof lists === 'function' ? await lists() : lists, kind, id, step);
      await refresh();
      return saved;
    } catch (error) {
      if (error instanceof OrderGoneError) {
        focus.current = null;
        await refresh();
        landing();
      }
      throw error;
    } finally {
      setMoving(false);
    }
  };
  return { message, moving, focus, announce: setMessage, send };
}

// OrderAnnouncement reads out where a row went, once per move.
export function OrderAnnouncement({ message }: { message: string }) {
  return <p className="sr-only" role="status" aria-live="polite">{message}</p>;
}
