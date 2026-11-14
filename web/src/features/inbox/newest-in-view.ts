import { useEffect, useLayoutEffect, useRef } from 'react';
import type { RefObject } from 'react';
import type { Message } from '../../api/schemas';

// A reader this close to the end of the timeline counts as being at the end.
const slack = 80;

interface Pane { scrollTop: number; clientHeight: number; scrollHeight: number }

export function isNearBottom(pane: Pane): boolean {
  return pane.scrollHeight - pane.scrollTop - pane.clientHeight <= slack;
}

interface Placement {
  // Where the newest message starts inside the scrolled content, and how tall it is.
  top: number;
  height: number;
  // The pane's visible height, the height of everything it scrolls, and the space it keeps around the messages.
  viewport: number;
  content: number;
  padding: number;
}

// newestOffset is the scroll offset that shows the newest message: its end at the end of the pane, or, when it
// is taller than the room the pane has for it, its start just under the pane's top.
export function newestOffset({ top, height, viewport, content, padding }: Placement): number {
  return height > viewport - 2 * padding ? Math.max(0, top - padding) : Math.max(0, content - viewport);
}

function place(pane: HTMLElement, newestId: string): void {
  // The message's article, or the line of an automatic reply that stands in for it while it is folded.
  const message = pane.querySelector<HTMLElement>(`[data-message-id="${newestId}"], [id="message-${newestId}"]`);
  if (!message) {
    pane.scrollTop = pane.scrollHeight;
    return;
  }
  const room = pane.getBoundingClientRect();
  const box = message.getBoundingClientRect();
  pane.scrollTop = newestOffset({ top: box.top - room.top + pane.scrollTop, height: box.height, viewport: pane.clientHeight, content: pane.scrollHeight,
    padding: Number.parseFloat(getComputedStyle(pane).paddingTop) || 0 });
}

interface Follow {
  conversationId: string;
  newestId: string;
  // The reader has not scrolled since the pane was placed: what loads late keeps the newest message in view.
  pinned: boolean;
  atBottom: boolean;
}

const ownMoves = ['wheel', 'touchmove', 'keydown', 'pointerdown'] as const;

// useNewestInView keeps the newest message of the open conversation where the reader sees it. It opens there
// (no animation, before the first paint) and shows what the reader just sent. A message that arrives later
// follows a reader who is at the end and never moves one who scrolled up. Late layout (a mail frame, an image, a
// chunk of the page) cannot push it out of view until the reader scrolls. A link to one message (#message-…)
// is that message's to place.
export function useNewestInView(pane: RefObject<HTMLElement | null>, conversationId: string | undefined, items: Message[], currentAgentId: string | undefined): void {
  const newest = items.at(-1);
  const newestId = newest && newest.conversation_id === conversationId ? newest.id : undefined;
  const sentByReader = Boolean(newest && currentAgentId && newest.kind !== 'incoming' && newest.author_agent_id === currentAgentId);
  const follow = useRef<Follow | undefined>(undefined);

  useLayoutEffect(() => {
    const box = pane.current;
    if (!box || !conversationId || !newestId) return;
    const was = follow.current;
    if (was?.conversationId !== conversationId) {
      const linked = window.location.hash.startsWith('#message-');
      const next: Follow = { conversationId, newestId, pinned: !linked, atBottom: false };
      follow.current = next;
      if (!linked) place(box, newestId);
      next.atBottom = isNearBottom(box);
      return;
    }
    if (was.newestId === newestId) return;
    was.newestId = newestId;
    if (sentByReader) {
      was.pinned = true;
      place(box, newestId);
    } else if (was.atBottom) {
      box.scrollTop = box.scrollHeight;
    } else {
      was.pinned = false;
    }
    was.atBottom = isNearBottom(box);
  }, [pane, conversationId, newestId, sentByReader]);

  useEffect(() => {
    const box = pane.current;
    if (!box || !conversationId) return;
    const leave = () => { if (follow.current) follow.current.pinned = false; };
    const track = () => { if (follow.current) follow.current.atBottom = isNearBottom(box); };
    const keep = () => {
      const was = follow.current;
      if (!was || was.conversationId !== conversationId) return;
      if (was.pinned) place(box, was.newestId);
      else if (was.atBottom) box.scrollTop = box.scrollHeight;
    };
    for (const name of ownMoves) box.addEventListener(name, leave, { passive: true });
    box.addEventListener('scroll', track, { passive: true });
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(keep);
    observer?.observe(box);
    if (box.firstElementChild) observer?.observe(box.firstElementChild);
    return () => {
      for (const name of ownMoves) box.removeEventListener(name, leave);
      box.removeEventListener('scroll', track);
      observer?.disconnect();
    };
  }, [pane, conversationId]);
}
