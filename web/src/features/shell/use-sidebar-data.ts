import { useCallback, useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { api } from '../../api/client';
import { savedViewsApi } from '../../api/saved-views';
import type { Me, TokenPermission } from '../../api/schemas';
import { can } from '../../lib/can';
import { apiQuery } from '../../lib/inbox-query';
import type { InboxQuery } from '../../lib/inbox-query';
import { channelItems, viewItems } from './sidebar-items';

// The inbox's first page, as the inbox itself asks for it: the sidebar's count of open conversations comes from its
// status counts, so on the inbox the count costs no request of its own.
const inboxFirstPage = apiQuery({}, 1);

// useIdle is true once the browser has nothing more urgent to do: the page's own requests go out first.
function useIdle() {
  const [idle, setIdle] = useState(false);
  useEffect(() => {
    if (window.requestIdleCallback) {
      const handle = window.requestIdleCallback(() => setIdle(true));
      return () => window.cancelIdleCallback(handle);
    }
    const timer = setTimeout(() => setIdle(true), 0);
    return () => clearTimeout(timer);
  }, []);
  return idle;
}

// useSidebarData reads what the sidebar lists: the open conversations, the saved views and the channels. Each is
// asked for only when the session may read it, after the page's own requests. current is the inbox's filters on
// screen, undefined on another page.
export function useSidebarData(me: Me | undefined, enabled: boolean, current: InboxQuery | undefined) {
  const router = useRouter();
  const idle = useIdle();
  const asks = (permission: TokenPermission) => enabled && idle && Boolean(me) && can(me, permission);
  const hrefOf = useCallback((query: InboxQuery) => router.buildLocation({ to: '/', search: query }).href, [router]);
  const inbox = useQuery({ queryKey: ['conversations', inboxFirstPage], queryFn: ({ signal }) => api.conversations(`${inboxFirstPage}&counts=true`, signal),
    enabled: asks('conversations:read') });
  const views = useQuery({ queryKey: ['saved-views'], queryFn: savedViewsApi.list, enabled: asks('saved_views:read') });
  const channels = useQuery({ queryKey: ['channels', 'counts'], queryFn: api.channelCounts, enabled: asks('channels:read') });
  // The Mentions list's first page, as Mentions asks for it: its unread count is the number beside Mentions.
  const mentions = useQuery({ queryKey: ['mentions', 'unread'], queryFn: () => api.mentions('unread'), enabled: asks('messages:read') });
  return {
    inboxOpen: inbox.data?.status_counts?.open,
    mentionsUnread: mentions.data?.unread,
    views: viewItems(views.data?.items ?? [], hrefOf, current),
    channels: channelItems(channels.data?.items ?? [], hrefOf, current),
  };
}
