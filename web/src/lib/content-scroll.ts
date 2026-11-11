import type { AnyRouter, ParsedLocation } from '@tanstack/react-router';

// On a wide screen a page scrolls inside the app's content pane (.app-content), not the window, so the router's
// own reset to the top (the window's) does not reach it: the offset of one page carried over to the next. This
// does for the pane what a browser does for a window: a page newly opened starts at its top, and Back or Forward
// returns to the offset that history entry was left at. A link to a section (#…) is left to the section, and a
// change of the address's query alone (a filter, an open item) keeps the offset. Offsets are kept for this tab
// while it is open.
const pane = () => document.querySelector<HTMLElement>('.app-content');
// Each history entry has its own key, so a second visit to the same page is a new entry, at its top.
const entry = (location: ParsedLocation) => (location.state as { __TSR_key?: string }).__TSR_key ?? location.href;

export function keepContentScroll(router: AnyRouter): () => void {
  const left = new Map<string, number>();
  const stopLeaving = router.subscribe('onBeforeLoad', ({ fromLocation, pathChanged }) => {
    const element = pane();
    if (fromLocation && pathChanged && element) left.set(entry(fromLocation), element.scrollTop);
  });
  const stopArriving = router.subscribe('onRendered', ({ toLocation, pathChanged }) => {
    const element = pane();
    if (!pathChanged || !element) return;
    const offset = left.get(entry(toLocation));
    if (offset !== undefined) element.scrollTop = offset;
    else if (!toLocation.hash) element.scrollTop = 0;
  });
  return () => { stopLeaving(); stopArriving(); };
}
