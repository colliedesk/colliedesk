import { useEffect, useRef } from 'react';

const ownMoves = ['wheel', 'touchmove', 'keydown', 'pointerdown'] as const;
const followMs = 10_000;

// useScrollToHashWhenReady goes to the section a link names (#…-heading) once `ready` says it is drawn.
// The router looks for it when the page renders, but a section drawn after its data or chunk arrives is
// not there yet, so it found nothing. Sections above it then fill in from their own requests and push it
// down, so it stays on it while the page grows, for a few seconds at most. It lets go for good, or never
// starts, once the person scrolls, types or clicks. The jump is instant, as the browser's own, so there is
// no motion to reduce, and focus stays where it is. `only` limits it to one section, for a part drawn on its own.
export function useScrollToHashWhenReady(ready: boolean, only?: string) {
  const moved = useRef(false);
  useEffect(() => {
    const move = () => { moved.current = true; };
    for (const event of ownMoves) window.addEventListener(event, move, { capture: true, passive: true });
    return () => { for (const event of ownMoves) window.removeEventListener(event, move, { capture: true }); };
  }, []);
  useEffect(() => {
    if (!ready) return;
    const hash = window.location.hash;
    const target = hash && (!only || hash === `#${only}`) ? document.getElementById(hash.slice(1)) : null;
    if (!target || moved.current) return;
    const go = () => { if (!moved.current && window.location.hash === hash) target.scrollIntoView?.(); };
    go();
    if (typeof ResizeObserver === 'undefined') return;
    const follow = new ResizeObserver(go);
    follow.observe(target.closest('main') ?? document.body);
    const stop = window.setTimeout(() => follow.disconnect(), followMs);
    return () => { follow.disconnect(); window.clearTimeout(stop); };
  }, [ready, only]);
}
