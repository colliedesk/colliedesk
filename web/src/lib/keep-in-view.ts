// keepInView brings the stretch of the page from top to bottom on screen, now and whenever the page
// changes size in the next ms (a timeline above it still reloading), and returns what stops it. Each end
// is scrolled into view in turn, the bottom last: when the stretch is taller than its scrolling box the
// bottom wins, which is where the actions are. Any scrolling box between them and the screen moves, the
// page on a phone or the reply pane on a short wide screen.
export function keepInView(top: Element | null | undefined, bottom: Element | null | undefined = top, ms = 2_000): () => void {
  const reveal = () => {
    top?.scrollIntoView?.({ block: 'nearest' });
    if (bottom !== top) bottom?.scrollIntoView?.({ block: 'nearest' });
  };
  reveal();
  if (typeof ResizeObserver === 'undefined') return () => {};
  // The page, and the box that holds the stretch, which grows on its own inside a scrolling pane.
  const page = new ResizeObserver(reveal);
  page.observe(document.body);
  if (top?.parentElement) page.observe(top.parentElement);
  const settled = setTimeout(() => page.disconnect(), ms);
  return () => {
    clearTimeout(settled);
    page.disconnect();
  };
}
