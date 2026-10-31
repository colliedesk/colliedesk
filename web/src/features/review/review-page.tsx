import type { ReactNode } from 'react';
import type { IntakeGathering, IntakeStats } from '../../api/intake';
import type { IntakeSummary } from '../../api/intake-summary';
import { Choice } from '../../ui/choice';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import { scopeName } from '../settings/ai-levels';
import { reviewMessages as m } from './review-messages';
import './review.css';

export type ReviewTab = 'waiting' | 'dismissed' | 'decided';

interface ReviewPageProps {
  // available is false when the workspace has no chat channel that collects messages.
  available: boolean;
  tab: ReviewTab;
  onTab: (tab: ReviewTab) => void;
  // filter is '' (every chat channel) or `<channel id>:<scope>`.
  filter: string;
  onFilter: (filter: string) => void;
  scopes: IntakeSummary['scopes'];
  waitingCount?: number;
  // decidingCount is how many cards wait for AI (intake on Auto) before the rules decide them.
  decidingCount?: number;
  stats?: IntakeStats;
  items?: IntakeGathering[];
  loading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  hasMore?: boolean;
  onMore?: () => void;
  renderCard: (item: IntakeGathering) => ReactNode;
  // notice says what the last decision on this page did; its card has left the list by then.
  notice?: ReactNode;
}

// A few channels and scopes are one press each; more become a list, which stays one line on a phone.
const maxChoices = 4;

const empty: Record<ReviewTab, string> = { waiting: m.emptyWaiting, dismissed: m.emptyDismissed, decided: m.emptyDecided };

// inLaneOrder puts a card that waits behind an earlier card of the same customer and place right under
// that card, so the two are decided in the order they were written.
export function inLaneOrder(items: IntakeGathering[]) {
  const ordered: IntakeGathering[] = [];
  for (const item of items) {
    let at = -1;
    if (item.review_reason === 'lane_blocked') ordered.forEach((entry, index) => { if (entry.lane_key === item.lane_key) at = index; });
    if (at === -1) ordered.push(item);
    else ordered.splice(at + 1, 0, item);
  }
  return ordered;
}

function StatsLine({ stats }: { stats: IntakeStats }) {
  if (stats.gatherings === 0) return <p className="review-stats muted-copy">{m.statsNone}</p>;
  const opened = (stats.by_state.created ?? 0) + (stats.by_state.attached ?? 0);
  return <p className="review-stats muted-copy"><span>{m.stats(stats.gatherings, opened)}</span>
    {stats.ai_decisions > 0 && <span>{m.statsCorrected(stats.ai_corrected, stats.ai_decisions)}</span>}</p>;
}

// ReviewPage is the intake queue of chat channels (docs/user/review.md): Waiting, what AI set aside, and
// what was decided, filtered by channel and scope.
export function ReviewPage({ available, tab, onTab, filter, onFilter, scopes, waitingCount, decidingCount, stats, items, loading, failed, onRetry, hasMore, onMore, renderCard, notice }: ReviewPageProps) {
  const shown = tab === 'waiting' && items ? inLaneOrder(items) : items ?? [];
  const filters = [{ value: '', label: m.allChannels }, ...scopes.map((entry) => ({ value: `${entry.channel_id}:${entry.scope}`, label: m.place(entry.channel_name, scopeName(entry.scope)) }))];
  return (
    <main className="settings-page review-page">
      <header>
        <h1>{m.title}</h1>
        <p>{m.lead}</p>
      </header>
      {!available ? <p className="empty-copy">{m.noChatChannels}</p> : <>
        {stats && <StatsLine stats={stats} />}
        <div className="review-bar">
          <Tabs prefix="review" label={m.tabs} value={tab} onChange={onTab}
            tabs={[{ id: 'waiting', label: m.waiting, count: waitingCount }, { id: 'dismissed', label: m.aiDismissed }, { id: 'decided', label: m.decided }]} />
          {filters.length > 2 && (filters.length <= maxChoices
            ? <Choice legend={m.filter} hideLegend value={filter} onChange={onFilter} items={filters} />
            : <label className="review-filter"><span className="sr-only">{m.filter}</span>
              <select value={filter} onChange={(event) => onFilter(event.target.value)}>
                {filters.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
              </select></label>)}
        </div>
        <p role="status" className="review-notice">{notice}</p>
        <section role="tabpanel" id={panelID('review', tab)} aria-labelledby={tabID('review', tab)} className="intake-list">
          {tab === 'waiting' && decidingCount !== undefined && decidingCount > 0 && <p className="muted-copy">{m.deciding(decidingCount)}</p>}
          {loading && !items && <p role="status" className="empty-copy">{m.loading}</p>}
          {failed && <div className="empty-copy"><p role="alert" className="error-copy">{m.failed}</p>
            <button type="button" onClick={onRetry}>{m.tryAgain}</button></div>}
          {items && items.length === 0 && !failed && <p className="empty-copy">{empty[tab]}</p>}
          {shown.map((item) => renderCard(item))}
          {hasMore && <button type="button" className="review-more" onClick={onMore}>{m.loadMore}</button>}
        </section>
      </>}
    </main>
  );
}
