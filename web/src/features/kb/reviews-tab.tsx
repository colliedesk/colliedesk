import { useQuery } from '@tanstack/react-query';
import { useHolds } from '../../lib/permissions-context';
import { kbApi } from './kb-api';
import { openForMeKey, reviewApi } from './review-api';
import { reviewMessages as r } from './review-messages';

// ReviewsTab opens the Reviews page, with how many texts wait for me. A workspace where nothing requires
// approval and nothing waits does not show it.
export function ReviewsTab({ current }: { current: boolean }) {
  const waiting = useQuery({ queryKey: openForMeKey, queryFn: () => reviewApi.reviews({ state: 'open', reviewer: 'me' }), retry: false, staleTime: 30_000 });
  const holds = useHolds();
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter, enabled: holds('help_center:read'), retry: false, staleTime: 60_000 });
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections, retry: false, staleTime: 60_000 });
  const count = waiting.data?.reviews.length ?? 0;
  const used = current || count > 0 || center.data?.require_approval === true || (collections.data?.items ?? []).some((item) => item.require_approval === 'on');
  if (!used) return null;
  return <a href="/knowledge/reviews" aria-current={current ? 'page' : undefined}>{r.reviewsTab}
    {count > 0 && <span className="kb-review-badge"><span aria-hidden="true">{count.toLocaleString('en')}</span><span className="sr-only">, {r.waitingCount(count)}</span></span>}</a>;
}
