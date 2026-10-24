import type { KeyboardEvent } from 'react';
import './controls.css';

export interface TabItem<T extends string> { id: T; label: string; count?: number }

// tabID and panelID name a tab and its panel, so a page's panel can point back at its tab.
export const tabID = (prefix: string, id: string) => `${prefix}-${id}`;
export const panelID = (prefix: string, id: string) => `${prefix}-${id}-panel`;

// Tabs is a WAI-ARIA tablist: one tab in the Tab order, the arrow keys move between them.
export function Tabs<T extends string>({ label, tabs, value, onChange, prefix }: { label: string; tabs: TabItem<T>[]; value: T; onChange: (id: T) => void; prefix: string }) {
  const keys = (event: KeyboardEvent) => {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
    const next = tabs[(tabs.findIndex((t) => t.id === value) + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
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
