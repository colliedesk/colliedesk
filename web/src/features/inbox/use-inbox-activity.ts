import { useEffect, useMemo, useRef } from 'react';
import { skipToken, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ConversationViewer } from '../../api/schemas';
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

// A customer message is announced for my conversations, unassigned ones and the one I have open; a
// teammate's conversation is otherwise theirs to answer. Items already announced as new or assigned
// in the same poll are not announced a second time for the message that created them.
function alerts(activity: Omit<Activity, 'updated' | 'viewing'>, replies: Activity['updated'], agentId: string | undefined, viewing: (id: string) => boolean): Alert[] {
  const body = (item: { subject: string; contact_name: string }) => `${item.subject} · ${item.contact_name}`;
  const assigned = activity.assigned_to_me.map((item) => ({ id: item.id, title: item.by_ai ? messages.notificationAssignedByAI : messages.notificationAssigned, body: body(item) }));
  const taken = new Set(assigned.map((item) => item.id));
  const created = activity.new_conversations.filter((item) => !taken.has(item.id))
    .map((item) => ({ id: item.id, title: messages.notificationNew, body: body(item) }));
  for (const item of created) taken.add(item.id);
  const replied = replies.filter((item) => !taken.has(item.id) && (item.assignee_id === null || item.assignee_id === agentId || viewing(item.id)))
    .map((item) => ({ id: item.id, title: messages.notificationReply, body: body(item) }));
  return [...created, ...assigned, ...replied];
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

// useInboxActivity keeps the signed-in app current: it polls for new, newly assigned and changed
// conversations, reloads the list and the timelines of the changed ones, and, while the tab is in
// the background, raises a browser notification (only when the agent turned them on) and shows the
// count in the tab title. agentId is the signed-in agent.
export function useInboxActivity(enabled = true, agentId?: string) {
  const client = useQueryClient();
  const cursor = useRef<string | undefined>(undefined);
  // The first answer only sets the cursor. Anything stamped before it was already in the list the
  // agent loaded, so the lookback must not announce it.
  const opened = useRef<number | undefined>(undefined);
  const seen = useRef({ created: new SeenIds(), assigned: new SeenIds(), updated: new SeenIds(), incoming: new SeenIds() });
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
    // An update repeats while it sits inside the server's lookback; it is new again only when the
    // conversation changed again (updated_at) or another customer message arrived (incoming_at).
    const changes = activity.data.updated.filter((item) => seen.current.updated.fresh(`${item.id}@${item.updated_at}`) && Date.parse(item.updated_at) > after);
    const replies = changes.filter((item) => item.incoming_at !== null && seen.current.incoming.fresh(`${item.id}@${item.incoming_at}`) && Date.parse(item.incoming_at) > after);
    // A timeline with an observer is on screen: the agent is reading that conversation.
    const viewing = (id: string) => (client.getQueryCache().find({ queryKey: ['messages', id], exact: true })?.getObserversCount() ?? 0) > 0;
    const items = alerts(unseen, replies, agentId, viewing);
    // An update does not say who made it, so the caller's own change also refreshes the list once
    // more. That costs at most one list fetch per poll and keeps the server row free of an actor field.
    if (items.length > 0 || changes.length > 0) void client.invalidateQueries({ queryKey: ['conversations'] });
    for (const item of changes) void client.invalidateQueries({ queryKey: ['messages', item.id] });
    if (items.length === 0) return;
    if (document.hasFocus()) return;
    if (pending.current === 0) baseTitle.current = document.title;
    pending.current += items.length;
    document.title = `(${pending.current}) ${baseTitle.current}`;
    if (currentNotificationState() === 'on') notify(items);
  }, [activity.data, agentId, client]);

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

const noViewing: Activity['viewing'] = [];

// useInboxViewing reads who else has which conversation open from the poll useInboxActivity runs for
// the whole app (every answer carries it, the first one included); it never polls on its own.
export function useInboxViewing(): ReadonlyMap<string, ConversationViewer[]> {
  const viewing = useQuery({ queryKey: ['inbox-activity'], queryFn: skipToken, select: (data: Activity) => data.viewing ?? noViewing }).data;
  return useMemo(() => new Map((viewing ?? noViewing).map((item) => [item.conversation_id, item.viewers])), [viewing]);
}
