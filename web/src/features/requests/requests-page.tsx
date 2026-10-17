import type { AgentRequest } from '../../api/schemas';
import { messages } from '../../messages';
import { RequestCard } from './request-card';
import type { ApproveOutcome } from './request-card';

export type RequestFilter = 'pending' | 'all';

interface RequestsPageProps {
  items?: AgentRequest[];
  // Approvals of this visit, shown first so a credential or link they answered stays readable.
  approved?: ApproveOutcome[];
  filter: RequestFilter;
  onFilter: (filter: RequestFilter) => void;
  timezone: string;
  loading?: boolean;
  failed?: boolean;
  onApprove: (id: string) => Promise<ApproveOutcome>;
  onReject: (id: string, reason?: string) => Promise<unknown>;
}

export function RequestsPage({ items = [], approved = [], filter, onFilter, timezone, loading, failed, onApprove, onReject }: RequestsPageProps) {
  const shown = new Set(approved.map((outcome) => outcome.request.id));
  const rest = items.filter((item) => !shown.has(item.id));
  const empty = !loading && !failed && rest.length === 0 && approved.length === 0;
  return (
    <main className="settings-page requests-page">
      <header>
        <h1>{messages.requests}</h1>
        <p>{messages.requestsDescription}</p>
      </header>
      <fieldset className="segmented" aria-label={messages.requestsFilter}>
        {(['pending', 'all'] as const).map((value) => (
          <label key={value}><input type="radio" name="request-filter" checked={filter === value} onChange={() => onFilter(value)} />
            <span>{value === 'pending' ? messages.requestsPending : messages.requestsAll}</span></label>
        ))}
      </fieldset>
      {loading && <p role="status" className="empty-copy">{messages.loading}</p>}
      {failed && <p role="alert" className="error-copy">{messages.requestsFailed}</p>}
      {empty && <p className="empty-copy">{filter === 'pending' ? messages.noPendingRequests : messages.noRequests}</p>}
      <div className="request-list">
        {approved.map((outcome) => <RequestCard key={outcome.request.id} item={outcome.request} initialOutcome={outcome} timezone={timezone} onApprove={onApprove} onReject={onReject} />)}
        {rest.map((item) => <RequestCard key={item.id} item={item} timezone={timezone} onApprove={onApprove} onReject={onReject} />)}
      </div>
    </main>
  );
}
