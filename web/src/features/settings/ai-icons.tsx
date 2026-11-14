import type { ReactNode } from 'react';

// Icons only this page uses stay in its chunk.
export function Glyph({ children }: { children: ReactNode }) {
  return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">{children}</svg>;
}
export const PlusIcon = () => <Glyph><path d="M12 5v14M5 12h14" /></Glyph>;
export const KeyIcon = () => <Glyph><circle cx="7.5" cy="15.5" r="4.5" /><path d="m10.7 12.3 9.8-9.8M17 6l3 3M14.5 8.5l2 2" /></Glyph>;
export const TrashIcon = () => <Glyph><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></Glyph>;
export const ServerIcon = () => <Glyph><rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" /><path d="M7 7.5h.01M7 16.5h.01" /></Glyph>;
export const InfoIcon = () => <Glyph><circle cx="12" cy="12" r="9" /><path d="M12 11v5M12 8h.01" /></Glyph>;
