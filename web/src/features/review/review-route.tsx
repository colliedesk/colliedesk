import { useState } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ConflictError } from '../../api/client';
import { intakeApi } from '../../api/intake';
import { useCreateAction } from '../../api/use-create';
import type { IntakeDecideInput, IntakeGathering, IntakeGatheringDetail, IntakeListQuery } from '../../api/intake';
import { intakeSummaryKey, useIntakeSummary } from './use-intake-summary';
import { IntakeCard } from './intake-card';
import { ReviewPage } from './review-page';
import type { ReviewTab } from './review-page';
import { reviewMessages as m } from './review-messages';

// The lists a tab reads: AI dismissed is what AI set aside; Decided is every card that was decided.
const listOf: Record<ReviewTab, Pick<IntakeListQuery, 'state' | 'decided_by'>> = {
  waiting: { state: 'review' }, dismissed: { state: 'ignored', decided_by: 'ai' }, decided: { state: 'decided' },
};

// The same interval as the rail badge: a card that arrives while the page is open shows within 20 seconds.
const pollMilliseconds = 20_000;

interface ReviewRouteProps {
  // The tab and the channel filter are in the address (/review?tab=…&channel=…&scope=…).
  tab: ReviewTab;
  filter: string;
  onChange: (tab: ReviewTab, filter: string) => void;
}

export function ReviewRoute({ tab, filter, onChange }: ReviewRouteProps) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const summary = useIntakeSummary();
  const [channelID, scope] = filter ? filter.split(':') : [undefined, undefined];
  const list = useInfiniteQuery({
    queryKey: ['intake', tab, filter],
    queryFn: ({ pageParam }) => intakeApi.list({ ...listOf[tab], channel_id: channelID, scope, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    refetchInterval: tab === 'waiting' ? pollMilliseconds : false,
  });
  const stats = useQuery({ queryKey: ['intake-stats', channelID ?? ''], queryFn: () => intakeApi.stats(channelID), enabled: summary.data?.available === true });
  const [openID, setOpenID] = useState<string>();
  // One press is one decision: a press sent again (a double click, a retry) goes under the same key.
  const decide = useCreateAction(({ id, input }: { id: string; input: IntakeDecideInput }, key: string) => intakeApi.decide(id, input, key));
  const requeue = useCreateAction((id: string, key: string) => intakeApi.requeue(id, key));
  const detail = useQuery({ queryKey: ['intake-card', openID], queryFn: () => intakeApi.get(openID ?? ''), enabled: Boolean(openID) });
  // A decision changes the lists, the counts and the rail badge, whether it was saved here or refused because
  // someone decided first.
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['intake'] });
    void client.invalidateQueries({ queryKey: ['intake-card'] });
    void client.invalidateQueries({ queryKey: ['intake-stats'] });
    void client.invalidateQueries({ queryKey: intakeSummaryKey });
  };
  // What the last decision did, said once its card has left the list, with the conversation it opened or joined.
  const [done, setDone] = useState<{ text: string; conversationID: string | null }>();
  const after = async (item: IntakeGathering, call: Promise<IntakeGatheringDetail>, said: (name: string) => string) => {
    setDone(undefined);
    const name = item.sender.name ?? m.someone;
    try {
      const card = await call;
      setDone({ text: said(name), conversationID: card.state === 'created' || card.state === 'attached' ? card.conversation_id : null });
      if (openID === item.id) setOpenID(undefined);
    } catch (error) {
      // Someone (or AI, or the rules) decided the card first: it leaves the list on the refresh below, so the
      // page says who, with the conversation it opened or joined.
      const conflict = error instanceof ConflictError && error.detail?.code === 'gathering_decided' ? error.detail : undefined;
      if (!conflict) throw error;
      const by = conflict.decided_by === 'ai' ? m.ai : conflict.decided_by === 'rules' ? m.rules
        : names.get(typeof conflict.decided_by_agent_id === 'string' ? conflict.decided_by_agent_id : '') ?? m.aTeammate;
      setDone({ text: m.decidedElsewhere(by, name), conversationID: typeof conflict.conversation_id === 'string' ? conflict.conversation_id : null });
    } finally {
      refresh();
    }
  };
  const names = new Map((team.data?.items ?? []).map((agent) => [agent.id, agent.display_name || agent.email]));
  const channels = new Map((summary.data?.scopes ?? []).map((entry) => [entry.channel_id, entry.channel_name]));
  const items = list.data?.pages.flatMap((page) => page.items);
  const renderCard = (item: IntakeGathering) => {
    const open = openID === item.id;
    return <IntakeCard key={item.id} item={item} open={open} timezone={me.data?.timezone ?? 'UTC'}
      detail={open && detail.data?.id === item.id ? detail.data : undefined} detailFailed={open && detail.isError} onRetryDetail={() => void detail.refetch()}
      onToggle={() => setOpenID(open ? undefined : item.id)}
      channelName={(id) => channels.get(id) ?? ''} agentName={(id) => (id && names.get(id)) || m.aTeammate}
      onDecide={(input: IntakeDecideInput) => after(item, decide.run({ id: item.id, input }), m.done[input.action === 'create' ? 'created' : input.action === 'attach' ? 'attached' : 'ignored'])}
      onRequeue={() => after(item, requeue.run(item.id), m.done.requeued)} />;
  };
  return <ReviewPage available={summary.data?.available ?? true} tab={tab} onTab={(next) => onChange(next, filter)} filter={filter} onFilter={(next) => onChange(tab, next)}
    scopes={summary.data?.scopes ?? []} waitingCount={summary.data?.waiting} decidingCount={summary.data?.deciding} stats={stats.data} items={items} loading={list.isPending} failed={list.isError}
    onRetry={() => void list.refetch()} hasMore={list.hasNextPage} onMore={() => void list.fetchNextPage()} renderCard={renderCard}
    notice={done && <>{done.text}{done.conversationID && <a href={`/conversations/${done.conversationID}`}>{m.openConversation}</a>}</>} />;
}
