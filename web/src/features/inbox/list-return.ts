// On a phone the list and an opened conversation take turns on the screen (data-view in layout.css), and the
// page itself scrolls. Leaving the list for a conversation remembers, for this tab, what the way back needs:
// that the list is the history entry behind that conversation, and how far the page was scrolled. It lives
// outside React because the inbox and the conversation are two routes, so the component can be remounted.
const phone = '(max-width: 44rem)';

let behind: { conversationId: string; scrollY: number } | undefined;

const onPhone = () => window.matchMedia?.(phone).matches ?? false;

export function leaveList(conversationId: string): void {
  behind = { conversationId, scrollY: window.scrollY };
}

// listIsBehind says whether going back in the history returns to the list, rather than out of the app (a shared link).
export function listIsBehind(conversationId: string): boolean {
  return behind?.conversationId === conversationId;
}

// showThread starts an opened conversation at its title.
export function showThread(): void {
  if (onPhone()) window.scrollTo(0, 0);
}

// showList puts the list back where it was left, once. Wider screens scroll the list pane, not the page.
export function showList(): void {
  const left = behind;
  behind = undefined;
  if (left && onPhone()) window.scrollTo(0, left.scrollY);
}
