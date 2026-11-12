import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';
import { messages } from '../../messages';
import { clampWidth, foldBelow, sidebarWidth } from './sidebar-layout';
import type { SidebarLayout, useSidebarLayout } from './sidebar-layout';

// The folded rail's width; layout.css gives the same number to --sidebar-width.
const railWidth = 56;
const keyStep = 16;

// SidebarResizer is the sidebar's right edge on a desktop: drag it to resize, drag it far left to fold the sidebar
// into its rail, double-click to restore the default width. As a separator it also takes the arrow keys, Home and End,
// and Enter folds or opens it.
export function SidebarResizer({ layout, set, preview }: ReturnType<typeof useSidebarLayout>) {
  const drag = useRef<{ x: number; from: number; width: number; last: SidebarLayout } | null>(null);
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    if (!dragging) return;
    document.documentElement.setAttribute('data-sidebar-resizing', '');
    return () => document.documentElement.removeAttribute('data-sidebar-resizing');
  }, [dragging]);
  const at = (clientX: number): SidebarLayout => {
    const start = drag.current;
    if (!start) return layout;
    const raw = start.from + clientX - start.x;
    return raw < foldBelow ? { width: start.width, collapsed: true } : { width: clampWidth(raw), collapsed: false };
  };
  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { x: event.clientX, from: layout.collapsed ? railWidth : layout.width, width: layout.width, last: layout };
    setDragging(true);
  };
  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current.last = at(event.clientX);
    preview(drag.current.last);
  };
  const end = () => {
    if (!drag.current) return;
    set(drag.current.last);
    drag.current = null;
    setDragging(false);
  };
  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const next: Record<string, SidebarLayout | undefined> = {
      ArrowLeft: layout.collapsed ? undefined : layout.width - keyStep < sidebarWidth.min ? { ...layout, collapsed: true } : { ...layout, width: layout.width - keyStep },
      ArrowRight: layout.collapsed ? { ...layout, collapsed: false } : { ...layout, width: layout.width + keyStep },
      Home: { ...layout, collapsed: true },
      End: { width: sidebarWidth.max, collapsed: false },
      Enter: { ...layout, collapsed: !layout.collapsed },
    };
    const value = next[event.key];
    if (!value) return;
    event.preventDefault();
    set(value);
  };
  return <div className="sidebar-resizer" role="separator" aria-orientation="vertical" aria-label={messages.resizeSidebar} tabIndex={0}
    aria-valuemin={railWidth} aria-valuemax={sidebarWidth.max} aria-valuenow={layout.collapsed ? railWidth : layout.width}
    data-dragging={dragging || undefined}
    onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={end} onPointerCancel={end} onLostPointerCapture={end}
    onDoubleClick={() => set({ width: sidebarWidth.initial, collapsed: false })} onKeyDown={onKeyDown} />;
}
