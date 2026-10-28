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
  return <div className="seg-tabs" role="tablist" aria-label={label}>
    {tabs.map((item) => <button key={item.id} type="button" role="tab" id={tabID(prefix, item.id)} aria-selected={value === item.id}
      aria-controls={panelID(prefix, item.id)} tabIndex={value === item.id ? 0 : -1} onClick={() => onChange(item.id)} onKeyDown={keys}>
      {item.label}{item.count !== undefined && <span className="seg-count">{item.count}</span>}
    </button>)}
  </div>;
}
