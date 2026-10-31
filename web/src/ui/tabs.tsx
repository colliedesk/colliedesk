import { useCallback, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import './controls.css';

export interface TabItem<T extends string> { id: T; label: string; count?: number }

// tabID and panelID name a tab and its panel, so a page's panel can point back at its tab.
export const tabID = (prefix: string, id: string) => `${prefix}-${id}`;
export const panelID = (prefix: string, id: string) => `${prefix}-${id}-panel`;

// Tabs is a WAI-ARIA tablist: one tab in the Tab order, the arrow keys move between them, Home and End
// go to the first and the last.
export function Tabs<T extends string>({ label, tabs, value, onChange, prefix }: { label: string; tabs: TabItem<T>[]; value: T; onChange: (id: T) => void; prefix: string }) {
  const keys = (event: KeyboardEvent) => {
    const at = tabs.findIndex((t) => t.id === value);
    const index = { ArrowRight: (at + 1) % tabs.length, ArrowLeft: (at + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
    if (index === undefined) return;
    event.preventDefault();
    const next = tabs[index];
    onChange(next.id);
    document.getElementById(tabID(prefix, next.id))?.focus();
  };
  const list = useRef<HTMLDivElement>(null);
  const [fade, setFade] = useState({ start: false, end: false });
  // A strip wider than its place scrolls sideways with no bar: the edge that hides tabs fades.
  const measure = useCallback(() => {
    const strip = list.current;
    if (!strip) return;
    const start = strip.scrollLeft > 1;
    const end = strip.scrollLeft + strip.clientWidth < strip.scrollWidth - 1;
    setFade((now) => (now.start === start && now.end === end ? now : { start, end }));
  }, []);
  useEffect(() => {
    const strip = list.current;
    if (!strip) return undefined;
    measure();
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(measure);
    observer?.observe(strip);
    return () => observer?.disconnect();
  }, [measure, tabs.length]);
  // The open tab is in sight, also when the address opened one past the edge.
  useEffect(() => {
    const strip = list.current;
    const open = strip?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (!strip || !open || strip.scrollWidth <= strip.clientWidth) return;
    if (open.offsetLeft < strip.scrollLeft || open.offsetLeft + open.offsetWidth > strip.scrollLeft + strip.clientWidth) {
      strip.scrollLeft = open.offsetLeft - (strip.clientWidth - open.offsetWidth) / 2;
    }
    measure();
  }, [value, measure]);
  const className = ['seg-tabs', fade.start && 'fade-start', fade.end && 'fade-end'].filter(Boolean).join(' ');
  return <div className={className} role="tablist" aria-label={label} ref={list} onScroll={measure}>
    {tabs.map((item) => <button key={item.id} type="button" role="tab" id={tabID(prefix, item.id)} aria-selected={value === item.id}
      aria-controls={panelID(prefix, item.id)} tabIndex={value === item.id ? 0 : -1} onClick={() => onChange(item.id)} onKeyDown={keys}>
      {item.label}{item.count !== undefined && <span className="seg-count">{item.count}</span>}
    </button>)}
  </div>;
}
