import type { SavedView } from '../../api/schemas';
import type { InboxQuery } from '../../lib/inbox-query';
import { queryOf } from '../../lib/inbox-query-edit';
import { onChannel, onView } from '../../lib/inbox-scope';

// What a saved view holds, shown as the dot before its name: open, waiting on the customer, urgent, or anything else.
export type ViewMark = 'open' | 'pending' | 'urgent' | 'none';

interface SidebarItem {
  key: string;
  label: string;
  // The inbox list it opens: the filters are in the address, so Back and a shared link open the same list.
  href: string;
  current: boolean;
}
export interface SidebarView extends SidebarItem { mark: ViewMark }
export interface SidebarChannel extends SidebarItem { type: string; warn: boolean }

// The part of a channel the sidebar reads (GET /api/v1/channels lists it).
export interface ChannelRow { id: string; name: string; type: string; status: { state: string } }

// hrefOf turns inbox filters into the inbox's address, as the router writes it.
type HrefOf = (query: InboxQuery) => string;

function markOf(view: Pick<SavedView, 'filters'>): ViewMark {
  const { priority, status } = view.filters;
  if (priority?.includes('urgent')) return 'urgent';
  if (status?.length === 1 && status[0] === 'pending') return 'pending';
  if (status?.length === 1 && status[0] === 'open') return 'open';
  return 'none';
}

// viewItems lists the saved views, each opening the inbox on its filters as the Views menu does. current is the
// inbox's filters on screen, undefined on another page. A view stays current while the agent changes its order or
// search, and its status when it stores none (onView).
export function viewItems(views: SavedView[], hrefOf: HrefOf, current: InboxQuery | undefined): SidebarView[] {
  return views.map((view) => {
    const query = queryOf(view.filters);
    return { key: view.id, label: view.name, href: hrefOf(query), mark: markOf(view), current: current !== undefined && onView(current, view.filters) };
  });
}

// channelItems lists the workspace's channels, each opening the inbox on its conversations. A removed channel takes
// no new mail, so it is not offered; one that needs attention carries a warning. A channel stays current under any
// status, order, search or Filter menu pick, and stops being current once a second channel joins it (onChannel).
export function channelItems(channels: ChannelRow[], hrefOf: HrefOf, current: InboxQuery | undefined): SidebarChannel[] {
  return channels.filter((channel) => channel.status.state !== 'removed').map((channel) => {
    const query: InboxQuery = { channel_id: channel.id };
    return { key: channel.id, label: channel.name, href: hrefOf(query), type: channel.type, warn: channel.status.state === 'needs_attention',
      current: current !== undefined && onChannel(current, channel.id) };
  });
}

// inboxIsCurrent: the sidebar marks Inbox as the place on screen only while no channel or view of it is, so one place
// is marked at a time.
export function inboxIsCurrent(views: SidebarItem[], channels: SidebarItem[]): boolean {
  return !views.some((item) => item.current) && !channels.some((item) => item.current);
}
