import { useEffect, useRef, useState } from 'react';
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

// sendMove moves id one place in its list (kind) and sends the order of everything under parent. The
// lists the page has may be stale or partial (a page of articles, or a collection whose articles it never
// read): the server then answers with the lists it has, and the move is made in those and sent once
// more. It returns the list of kind as it was saved.
export async function sendMove(parent: string | null, lists: OrderLists, kind: keyof OrderLists, id: string, step: Step): Promise<string[]> {
  const send = async (from: OrderLists) => {
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
}

// OrderButtons move a row one place up or down. They are plain buttons, so the keyboard reaches them;
// the result is read out through OrderAnnouncement, and the focus stays on the row that moved.
export function OrderButtons({ ids, id, name, more = false, busy = false, text = false, onMove, onAnnounce }: OrderButtonsProps) {
  const place = ids.indexOf(id);
  const up = useRef<HTMLButtonElement>(null);
  const down = useRef<HTMLButtonElement>(null);
  const [moving, setMoving] = useState(false);
  // The direction last clicked, until the list shows the row in its new place.
  const focusAfter = useRef<Step | null>(null);
  // The page builds ids anew on every render; only a new order moves the focus.
  const order = ids.join(' ');
  useEffect(() => {
    const step = focusAfter.current;
    if (step === null) return;
    focusAfter.current = null;
    (step === -1 ? up.current ?? down.current : down.current ?? up.current)?.focus();
  }, [order]);
  const move = async (step: Step) => {
    if (moving || busy) return;
    setMoving(true);
    focusAfter.current = step;
    try {
      const saved = await onMove(moveInOrder(ids, id, step), step) ?? moveInOrder(ids, id, step);
      onAnnounce(kbMessages.moved(name, saved.indexOf(id) + 1, saved.length));
    } catch {
      focusAfter.current = null;
      onAnnounce(kbMessages.moveFailed(name));
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

// useOrderMoves sends one move at a time, reads the lists again after it, and keeps what the last move
// should read out (OrderAnnouncement).
export function useOrderMoves(refresh: () => Promise<unknown>) {
  const [message, setMessage] = useState('');
  const [moving, setMoving] = useState(false);
  const send = async (parent: string | null, lists: OrderLists, kind: keyof OrderLists, id: string, step: Step) => {
    setMoving(true);
    try {
      const saved = await sendMove(parent, lists, kind, id, step);
      await refresh();
      return saved;
    } finally {
      setMoving(false);
    }
  };
  return { message, moving, announce: setMessage, send };
}

// OrderAnnouncement reads out where a row went, once per move.
export function OrderAnnouncement({ message }: { message: string }) {
  return <p className="sr-only" role="status" aria-live="polite">{message}</p>;
}
