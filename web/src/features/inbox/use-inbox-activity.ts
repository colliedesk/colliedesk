import { useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { messages } from '../../messages';
import { currentNotificationState } from '../shell/notification-preference';

const pollMilliseconds = 20_000;
// More items than this in one poll (a laptop that slept) become one summary notification.
const individualNotifications = 3;

// Ids already announced, per kind. The server repeats items inside its lookback window, so the
// browser removes duplicates; the cap keeps a long-lived tab from growing without bound.
const seenLimit = 500;

type Activity = Awaited<ReturnType<typeof api.inboxActivity>>;

class SeenIds {
  private readonly ids = new Set<string>();
  // fresh reports whether the id is new and remembers it.
  fresh(id: string): boolean {
    if (this.ids.has(id)) return false;
    this.ids.add(id);
    if (this.ids.size > seenLimit) this.ids.delete(this.ids.values().next().value as string);
    return true;
  }
}

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

// useInboxActivity keeps the signed-in app current: it polls for new and newly assigned conversations,
// reloads the list when there is any, and, while the tab is in the background, raises a browser
// notification (only when the agent turned them on) and shows the count in the tab title.
export function useInboxActivity(enabled = true) {
  const client = useQueryClient();
  const cursor = useRef<string | undefined>(undefined);
  // The first answer only sets the cursor. Anything stamped before it was already in the list the
  // agent loaded, so the lookback must not announce it.
  const opened = useRef<number | undefined>(undefined);
  const seen = useRef({ created: new SeenIds(), assigned: new SeenIds() });
  const pending = useRef(0);
  const baseTitle = useRef(document.title);
  const activity = useQuery({
    queryKey: ['inbox-activity'],
    queryFn: async () => {
      const result = await api.inboxActivity(cursor.current);
      cursor.current = result.now;
      return result;
    },
    enabled, refetchInterval: pollMilliseconds, refetchIntervalInBackground: true, staleTime: 0, gcTime: 0,
  });

  useEffect(() => {
    if (!activity.data) return;
    if (opened.current === undefined) {
      opened.current = Date.parse(activity.data.now);
      return;
    }
    const after = opened.current;
    const unseen = {
      now: activity.data.now,
      new_conversations: activity.data.new_conversations.filter((item) => seen.current.created.fresh(item.id) && Date.parse(item.created_at) > after),
      assigned_to_me: activity.data.assigned_to_me.filter((item) => seen.current.assigned.fresh(item.id) && Date.parse(item.assigned_at) > after),
    };
    const items = alerts(unseen);
    if (items.length === 0) return;
    void client.invalidateQueries({ queryKey: ['conversations'] });
    if (document.hasFocus()) return;
    if (pending.current === 0) baseTitle.current = document.title;
    pending.current += items.length;
    document.title = `(${pending.current}) ${baseTitle.current}`;
    if (currentNotificationState() === 'on') notify(items);
  }, [activity.data, client]);

  useEffect(() => {
    const clear = () => {
      if (pending.current === 0) return;
      pending.current = 0;
      document.title = baseTitle.current;
    };
    const onVisible = () => { if (document.visibilityState === 'visible') clear(); };
    window.addEventListener('focus', clear);
    document.addEventListener('visibilitychange', onVisible);
    return () => { window.removeEventListener('focus', clear); document.removeEventListener('visibilitychange', onVisible); clear(); };
  }, []);
}
