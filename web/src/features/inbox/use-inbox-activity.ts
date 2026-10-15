import { useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { messages } from '../../messages';
import { currentNotificationState } from '../shell/notification-preference';

const pollMilliseconds = 20_000;
// More items than this in one poll (a laptop that slept) become one summary notification.
const individualNotifications = 3;

type Activity = Awaited<ReturnType<typeof api.inboxActivity>>;

interface Alert { id: string; title: string; body: string }

function alerts(activity: Activity): Alert[] {
  const assigned = activity.assigned_to_me.map((item) => ({ id: item.id, title: messages.notificationAssigned, body: `${item.subject} · ${item.contact_name}` }));
  const taken = new Set(assigned.map((item) => item.id));
  const created = activity.new_conversations.filter((item) => !taken.has(item.id))
    .map((item) => ({ id: item.id, title: messages.notificationNew, body: `${item.subject} · ${item.contact_name}` }));
  return [...created, ...assigned];
}

function notify(items: Alert[]) {
  try {
    if (items.length > individualNotifications) {
      new Notification(messages.notificationSummary(items.length), { tag: 'colliedesk-summary' });
      return;
    }
    for (const item of items) new Notification(item.title, { body: item.body, tag: item.id });
  } catch { /* some mobile browsers only allow notifications from a service worker */ }
}

// useInboxActivity keeps an open inbox current: it polls for new and newly assigned conversations,
// reloads the list when there is any, and, while the tab is in the background, raises a browser
// notification (only when the agent turned them on) and shows the count in the tab title.
export function useInboxActivity() {
  const client = useQueryClient();
  const cursor = useRef<string | undefined>(undefined);
  const unseen = useRef(0);
  const baseTitle = useRef(document.title);
  const activity = useQuery({
    queryKey: ['inbox-activity'],
    queryFn: async () => {
      const result = await api.inboxActivity(cursor.current);
      cursor.current = result.now;
      return result;
    },
    refetchInterval: pollMilliseconds, refetchIntervalInBackground: true, staleTime: 0, gcTime: 0,
  });

  useEffect(() => {
    if (!activity.data) return;
    const items = alerts(activity.data);
    if (items.length === 0) return;
    void client.invalidateQueries({ queryKey: ['conversations'] });
    if (document.hasFocus()) return;
    if (unseen.current === 0) baseTitle.current = document.title;
    unseen.current += items.length;
    document.title = `(${unseen.current}) ${baseTitle.current}`;
    if (currentNotificationState() === 'on') notify(items);
  }, [activity.data, client]);

  useEffect(() => {
    const clear = () => {
      if (unseen.current === 0) return;
      unseen.current = 0;
      document.title = baseTitle.current;
    };
    const onVisible = () => { if (document.visibilityState === 'visible') clear(); };
    window.addEventListener('focus', clear);
    document.addEventListener('visibilitychange', onVisible);
    return () => { window.removeEventListener('focus', clear); document.removeEventListener('visibilitychange', onVisible); clear(); };
  }, []);
}
