import { useLayoutEffect, useRef, useState } from 'react';
import type { CSSProperties, RefObject } from 'react';

// Where a list that belongs to a character of a text box goes on the screen: at that character, like the @ list of a
// chat app.

// The line a character is drawn on, in screen pixels: left at the character, top and bottom of its whole line.
export interface CaretLine { left: number; top: number; bottom: number }

const copied = ['fontFamily', 'fontSize', 'fontStyle', 'fontVariant', 'fontWeight', 'fontStretch', 'fontFeatureSettings', 'fontKerning',
  'lineHeight', 'letterSpacing', 'wordSpacing', 'textIndent', 'textTransform', 'tabSize', 'direction', 'wordBreak',
  'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft'] as const;

// caretRect finds the line of the character at `index`. A text box has no API for where a character is drawn, so its text
// up to there goes into a hidden copy that wraps the same way (same width, padding, font and white-space rules), a span
// marks the spot, and the span's place is read. The span's text sits in the middle of the line's height.
export function caretRect(box: HTMLTextAreaElement, index: number): CaretLine {
  const style = getComputedStyle(box);
  const mirror = document.createElement('div');
  for (const name of copied) mirror.style[name] = style[name];
  // clientWidth leaves out the borders and a scrollbar, so the copy's lines break where the box's do.
  Object.assign(mirror.style, {
    position: 'absolute', top: '0', left: '-9999px', visibility: 'hidden', boxSizing: 'border-box', width: `${box.clientWidth}px`,
    border: '0', whiteSpace: 'pre-wrap', overflowWrap: 'break-word',
  });
  mirror.textContent = box.value.slice(0, index);
  const mark = document.createElement('span');
  // The rest of the text follows, so a word that does not fit on the line moves to the next one, as in the box.
  mark.textContent = box.value.slice(index) || '.';
  mirror.append(mark);
  document.body.append(mirror);
  try {
    const frame = mirror.getBoundingClientRect();
    const spot = mark.getClientRects()[0] ?? frame;
    const lineHeight = parseFloat(style.lineHeight) || spot.height;
    const area = box.getBoundingClientRect();
    const top = area.top + box.clientTop + (spot.top - frame.top) - (lineHeight - spot.height) / 2 - box.scrollTop;
    return { left: area.left + box.clientLeft + (spot.left - frame.left) - box.scrollLeft, top, bottom: top + lineHeight };
  } finally {
    mirror.remove();
  }
}

const gap = 4;
const edge = 8;

// placeAtCaret puts a list of `size` under the line, its left edge at the character; above the line when the screen has
// no room under it. It keeps 8 px free at each edge of the screen: it moves left when the character is near the right
// edge, and when neither side has room it takes the roomier one and stays inside the screen over the line.
export function placeAtCaret(line: CaretLine, size: { width: number; height: number }, screen: { width: number; height: number }): { left: number; top: number } {
  const below = line.bottom + gap;
  const above = line.top - gap - size.height;
  const roomBelow = screen.height - edge - below;
  const roomAbove = line.top - gap - edge;
  const fits = (room: number) => room >= size.height;
  const top = fits(roomBelow) ? below : fits(roomAbove) ? above
    : Math.max(edge, Math.min(roomBelow >= roomAbove ? below : above, screen.height - edge - size.height));
  const left = Math.max(edge, Math.min(line.left, screen.width - edge - size.width));
  return { left, top };
}

// useCaretAnchor places a fixed list (its ref) at the character `at` of the box, from before it is painted, and again
// when the box or the list changes size (the box grows as it takes the focus, the list as matches come and go), the
// window is resized or anything scrolls. Without `at` the list is closed and nothing is watched.
export function useCaretAnchor(box: RefObject<HTMLTextAreaElement | null>, at: { start: number; query: string } | undefined) {
  const list = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number }>();
  const start = at?.start;
  // The text typed after the @ can move it: a word too long for its line goes to the next one.
  const query = at?.query;
  useLayoutEffect(() => {
    const area = box.current;
    if (start === undefined || !area) { setPlace(undefined); return; }
    const update = () => {
      const node = list.current;
      if (!node) return;
      const next = placeAtCaret(caretRect(area, start), { width: node.offsetWidth, height: node.offsetHeight },
        { width: document.documentElement.clientWidth, height: window.innerHeight });
      setPlace((current) => (current?.left === next.left && current.top === next.top ? current : next));
    };
    update();
    const resized = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(update);
    resized?.observe(area);
    if (list.current) resized?.observe(list.current);
    window.addEventListener('resize', update);
    document.addEventListener('scroll', update, true);
    return () => {
      resized?.disconnect();
      window.removeEventListener('resize', update);
      document.removeEventListener('scroll', update, true);
    };
  }, [box, start, query]);
  // Until it is measured the list is laid out where it cannot be seen; that happens before the first paint.
  const style: CSSProperties = place ?? { left: 0, top: 0, visibility: 'hidden' };
  return { ref: list, style };
}
