import { useCallback, useEffect, useRef, useState } from 'react';
import { api, TooManyRequestsError } from '../../api/client';
import type { ConversationViewer } from '../../api/schemas';

// One id per page load, kept in memory only: "Duplicate tab" copies sessionStorage, and two tabs sharing
// an id would remove each other's presence when one of them closes.
export const presenceClientId = globalThis.crypto?.randomUUID?.() ?? `tab-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;

// A change of the reply box is said at once, but never twice within this long (the server takes a change
// half a second after the last announce, a repeat after two).
const minimumGap = 1_000;
// After a refused or failed announce, the next try waits this long.
const retryAfter = 10_000;

const nobody: ConversationViewer[] = [];

// useConversationPresence tells the team that this tab has a conversation open, and whether its reply
// box holds a reply not sent yet (writing), and returns the others who have it open. The first announce
// waits for the requests the page started for the ticket, so presence never delays opening it. A tab in
// the background leaves at once unless a reply is being written there, which then stays listed at the
// slower pace the server asks for; closing the page, opening another conversation and leaving the inbox
// leave at once. markEdited says the reply text changed, with the next announce.
export function useConversationPresence(conversationId: string | undefined, writing: boolean) {
  const [answer, setAnswer] = useState<{ id: string; viewers: ConversationViewer[] }>();
  const writingNow = useRef(writing);
  const edited = useRef(false);
  const announceNow = useRef<() => void>(() => {});

  useEffect(() => {
    if (!conversationId) return;
    const id = conversationId;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let last = Number.NEGATIVE_INFINITY;
    let announced = false;
    let stopped = false;
    let sequence = 0;
    const hidden = () => document.visibilityState === 'hidden';
    const later = (ms: number) => { clearTimeout(timer); timer = setTimeout(announce, ms); };
    const leave = () => {
      clearTimeout(timer);
      if (!announced) return;
      announced = false;
      void api.leavePresence(id, presenceClientId).catch(() => {});
    };
    function announce() {
      if (stopped) return;
      if (hidden() && !writingNow.current) { leave(); return; }
      const wait = last + minimumGap - Date.now();
      if (wait > 0) { later(wait); return; }
      clearTimeout(timer);
      last = Date.now();
      announced = true;
      const mine = ++sequence;
      const body = { client_id: presenceClientId, writing: writingNow.current, edited: edited.current, hidden: hidden() };
      edited.current = false;
      api.announcePresence(id, body).then((result) => {
        if (stopped || mine !== sequence) return;
        // The tab left while this announce was on its way: the leave may have arrived first.
        if (!announced) { void api.leavePresence(id, presenceClientId).catch(() => {}); return; }
        setAnswer({ id, viewers: result.viewers });
        later(result.heartbeat_seconds * 1_000);
      }, (error: unknown) => {
        if (stopped || mine !== sequence || !announced) return;
        later(error instanceof TooManyRequestsError ? error.retryAfterSeconds * 1_000 : retryAfter);
      });
    }
    announceNow.current = announce;
    const onPageShow = (event: PageTransitionEvent) => { if (event.persisted) announce(); };
    // Going to the background with a reply being written announces nothing extra: the next heartbeat says
    // hidden. An announce sent now could reach the server after the leave of a tab that is closing.
    const onVisibility = () => {
      if (!hidden()) announce();
      else if (!writingNow.current) leave();
    };
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('pagehide', leave);
    window.addEventListener('pageshow', onPageShow);
    // After the ticket's own requests: a browser has six connections per host for them.
    later(0);
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('pagehide', leave);
      window.removeEventListener('pageshow', onPageShow);
      leave();
      stopped = true;
      announceNow.current = () => {};
    };
  }, [conversationId]);

  useEffect(() => {
    if (writingNow.current === writing) return;
    writingNow.current = writing;
    announceNow.current();
  }, [writing]);

  const markEdited = useCallback(() => { edited.current = true; }, []);
  return { viewers: answer && answer.id === conversationId ? answer.viewers : nobody, markEdited };
}
