// The conversations and customers a person opened last, kept by this browser for the search palette's Recent group.
// Each workspace keeps its own list, so a second workspace in the same browser never shows the first one's names.
export type RecentKind = 'conversation' | 'customer';
export interface RecentItem { kind: RecentKind; id: string; title: string; detail?: string }

const most = 5;
const keyOf = (workspaceId: string) => `colliedesk.recent.${workspaceId}`;

const isItem = (value: unknown): value is RecentItem => {
  const item = value as Partial<RecentItem> | null;
  return typeof item === 'object' && item !== null && (item.kind === 'conversation' || item.kind === 'customer')
    && typeof item.id === 'string' && typeof item.title === 'string' && (item.detail === undefined || typeof item.detail === 'string');
};

// readRecent lists them newest first; storage that is off, full or holds something else reads as nothing.
export function readRecent(workspaceId: string): RecentItem[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(keyOf(workspaceId)) ?? '[]');
    return Array.isArray(stored) ? stored.filter(isItem).slice(0, most) : [];
  } catch {
    return [];
  }
}

// rememberRecent puts an item at the top; opened again, it moves up instead of showing twice.
export function rememberRecent(workspaceId: string, item: RecentItem) {
  const others = readRecent(workspaceId).filter((kept) => kept.kind !== item.kind || kept.id !== item.id);
  try {
    localStorage.setItem(keyOf(workspaceId), JSON.stringify([item, ...others].slice(0, most)));
  } catch { /* the list is a convenience: without storage it stays as it was */ }
}

export const recentHref = (item: RecentItem) => (item.kind === 'conversation' ? `/conversations/${item.id}` : `/contacts/${item.id}`);
