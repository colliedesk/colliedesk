import { useCallback, useState } from 'react';

// The desktop sidebar's width and whether it is folded into an icon rail, kept per browser like the theme.
export const sidebarStorageKey = 'colliedesk.sidebar';
export const sidebarWidth = { min: 200, max: 360, initial: 248 } as const;
// Dragged narrower than this, the sidebar folds into the rail; dragged wider from the rail, it opens again.
export const foldBelow = 160;
// A browser that never chose starts open, so a new person sees each page's name; the rail is one click away.
const collapsedByDefault = false;

export interface SidebarLayout { width: number; collapsed: boolean }

export const clampWidth = (width: number) => Math.round(Math.min(sidebarWidth.max, Math.max(sidebarWidth.min, width)));

export function readSidebarLayout(storage: Pick<Storage, 'getItem'> | undefined = defaultStorage()): SidebarLayout {
  try {
    const stored: unknown = JSON.parse(storage?.getItem(sidebarStorageKey) ?? 'null');
    if (stored && typeof stored === 'object') {
      const { width, collapsed } = stored as Partial<SidebarLayout>;
      return { width: typeof width === 'number' && Number.isFinite(width) ? clampWidth(width) : sidebarWidth.initial, collapsed: typeof collapsed === 'boolean' ? collapsed : collapsedByDefault };
    }
  } catch { /* unreadable or blocked: the default layout */ }
  return { width: sidebarWidth.initial, collapsed: collapsedByDefault };
}

export function saveSidebarLayout(layout: SidebarLayout, storage: Pick<Storage, 'setItem'> | undefined = defaultStorage()) {
  try { storage?.setItem(sidebarStorageKey, JSON.stringify(layout)); } catch { /* storage blocked: the layout lasts until reload */ }
}

function defaultStorage(): Storage | undefined {
  try { return window.localStorage; } catch { return undefined; }
}

// useSidebarLayout keeps the layout in state and in storage. preview changes the width while a drag runs and saves
// nothing; set saves.
export function useSidebarLayout() {
  const [layout, setLayout] = useState(readSidebarLayout);
  const set = useCallback((next: SidebarLayout) => {
    const value = { width: clampWidth(next.width), collapsed: next.collapsed };
    setLayout(value);
    saveSidebarLayout(value);
  }, []);
  const preview = useCallback((next: SidebarLayout) => setLayout({ width: clampWidth(next.width), collapsed: next.collapsed }), []);
  return { layout, set, preview };
}
