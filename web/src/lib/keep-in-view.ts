// keepInView brings the stretch of the page from top to bottom on screen, now and whenever the page
// changes size in the next ms (a timeline above it still reloading), and returns what stops it. When the
// stretch is taller than the screen its bottom wins: that is where the actions are. Only the page scrolls
// here; on a wide screen these parts sit in panes that are already on screen and nothing moves.
export function keepInView(top: Element | null | undefined, bottom: Element | null | undefined = top, ms = 2_000): () => void {
  const reveal = () => {
    if (!top || !bottom) return;
    const first = top.getBoundingClientRect().top;
    const last = bottom.getBoundingClientRect().bottom;
    const below = last - window.innerHeight;
    const delta = below > 0 ? (last - first <= window.innerHeight ? Math.min(below, first) : below) : Math.min(first, 0);
    if (delta !== 0) window.scrollBy(0, delta);
  };
  reveal();
  if (typeof ResizeObserver === 'undefined') return () => {};
  const page = new ResizeObserver(reveal);
  page.observe(document.body);
  const settled = setTimeout(() => page.disconnect(), ms);
  return () => {
    clearTimeout(settled);
    page.disconnect();
  };
}
