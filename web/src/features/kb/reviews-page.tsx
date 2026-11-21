import { useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCreateAction } from '../../api/use-create';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { kbApi } from './kb-api';
import type { KBArticleSummary } from './kb-api';
import { reason } from './kb-tree';
import { KBHead } from './kb-ui';
import { openForMeKey, reviewApi } from './review-api';
import type { BulkResult, KBReview } from './review-api';
import { reviewMessages as r } from './review-messages';

type View = 'waiting' | 'open' | 'approved';
const views: [View, string][] = [['waiting', r.waitingView], ['open', r.openView], ['approved', r.approvedView]];

// ReviewsRoute: what waits for my approval, every text in review, and the approved text that waits to be
// published. Each list takes several at once; the answer says, per text, what was done or refused and why.
export function ReviewsRoute() {
  const [view, setView] = useState<View>('waiting');
  return (
    <main className="kb-frame kb-languages-page kb-reviews-page" aria-labelledby="kb-heading">
      <KBHead current="reviews" heading={r.heading} />
      <div className="kb-languages-scroll">
        <section className="kb-languages kb-reviews" aria-labelledby="kb-reviews-heading">
          <h2 id="kb-reviews-heading">{r.views}</h2>
          <fieldset className="segmented">
            <legend className="sr-only">{r.views}</legend>
            {views.map(([value, label]) => (
              <label key={value}><input type="radio" name="kb-review-view" checked={view === value} onChange={() => setView(value)} />{label}</label>
            ))}
          </fieldset>
          {view === 'approved' ? <ApprovedList key={view} /> : <ReviewList key={view} mine={view === 'waiting'} />}
        </section>
      </div>
    </main>
  );
}

function ReviewList({ mine }: { mine: boolean }) {
  const client = useQueryClient();
  const holds = useHolds();
  const list = useQuery({ queryKey: mine ? openForMeKey : ['kb-reviews', 'open', 'all'], queryFn: () => reviewApi.reviews(mine ? { state: 'open', reviewer: 'me' } : { state: 'open' }) });
  const approve = useCreateAction((ids: string[], key: string) => reviewApi.approveMany(ids, key));
  const items = list.data?.reviews ?? [];
  return <Bulk label={mine ? r.waitingView : r.openView} empty={mine ? r.emptyWaiting : r.emptyOpen} query={list} rows={items.map((review) => ({ id: review.id, title: review.article_title, body: <ReviewRow review={review} /> }))}
    action={holds('kb_articles:publish') ? r.approveSelected : undefined}
    run={async (ids) => {
      const done = await approve.run(ids);
      await client.invalidateQueries({ queryKey: ['kb-reviews'] });
      void client.invalidateQueries({ queryKey: ['kb-articles'] });
      return done.results;
    }} />;
}

function ApprovedList() {
  const client = useQueryClient();
  const holds = useHolds();
  const list = useQuery({ queryKey: ['kb-articles', 'approved'], queryFn: () => kbApi.articles({ state: 'approved', limit: 200 }) });
  const publish = useCreateAction((ids: string[], key: string) => reviewApi.publishMany(ids, key));
  const items = list.data?.items ?? [];
  return <Bulk label={r.approvedView} empty={r.emptyApproved} query={list} rows={items.map((article) => ({ id: article.id, title: article.title, body: <ArticleRow article={article} /> }))}
    action={holds('kb_articles:publish') ? r.publishSelected : undefined}
    run={async (ids) => {
      const done = await publish.run(ids);
      await client.invalidateQueries({ queryKey: ['kb-articles'] });
      void client.invalidateQueries({ queryKey: ['kb-reviews'] });
      return done.results;
    }} />;
}

function ReviewRow({ review }: { review: KBReview }) {
  return <div className="kb-template-text">
    <a href={`/knowledge/reviews/${review.id}`}><strong>{review.article_title}</strong></a>
    {review.locale && <span>{r.translation(review.locale)}</span>}
    {review.note && <span>{review.note}</span>}
    <small className="muted-copy">{r.sentBy(review.requested_by?.name ?? '')}</small>
    {review.requested_by_token && <small className="muted-copy">{r.byAgent(review.requested_by_token.label)}</small>}
    <small className="muted-copy">{r.reviewersAre(review.reviewers)}</small>
  </div>;
}

function ArticleRow({ article }: { article: KBArticleSummary }) {
  return <div className="kb-template-text">
    <a href={`/knowledge/articles/${article.id}`}><strong>{article.title}</strong></a>
    {article.last_change && <small className="muted-copy">{article.last_change.changed_by?.name}</small>}
  </div>;
}

interface BulkProps {
  label: string;
  empty: string;
  query: { isPending: boolean; isError: boolean; isSuccess: boolean; refetch: () => unknown };
  rows: { id: string; title: string; body: ReactNode }[];
  // action names the button for a count; without it the list cannot be acted on (no kb_articles:publish).
  action?: (count: number) => string;
  run: (ids: string[]) => Promise<BulkResult[]>;
}

// Bulk is a list with a checkbox per row, Select all, one action for the chosen rows and its answer per row.
function Bulk({ label, empty, query, rows, action, run }: BulkProps) {
  const [chosen, setChosen] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [answer, setAnswer] = useState<{ done: number; refused: string[] }>();
  const [problem, setProblem] = useState<string>();
  const titles = new Map(rows.map((row) => [row.id, row.title]));
  const all = rows.length > 0 && rows.every((row) => chosen.includes(row.id));
  const press = async () => {
    setBusy(true);
    setProblem(undefined);
    try {
      const results = await run(chosen);
      const refused = results.filter((result) => result.error);
      setAnswer({ done: results.length - refused.length, refused: refused.map((result) => `${titles.get(result.id) ?? result.id}: ${result.error?.message ?? ''}`) });
      setChosen([]);
    } catch (error) {
      setProblem(reason(error, r.actionFailed));
    } finally {
      setBusy(false);
    }
  };
  return <>
    {query.isError && <p role="alert" className="error-copy">{r.loadFailed}</p>}
    {query.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
    <p role="status" aria-live="polite" className={answer ? 'muted-copy' : 'sr-only'}>{answer && r.bulkDone(answer.done, answer.refused.length)}</p>
    {answer && answer.refused.length > 0 && <ul className="kb-review-refused">{answer.refused.map((line) => <li key={line}>{line}</li>)}</ul>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
    {query.isSuccess && rows.length === 0 && <p className="empty-copy">{empty}</p>}
    {rows.length > 0 && <>
      {action && <div className="button-row kb-review-bulk">
        <label className="check-row"><input type="checkbox" checked={all} onChange={(event) => setChosen(event.target.checked ? rows.map((row) => row.id) : [])} /><span>{r.selectAll}</span></label>
        <button type="button" className="primary-action" disabled={busy || chosen.length === 0} onClick={() => void press()}>{action(chosen.length)}</button>
      </div>}
      <ul className="kb-template-list" aria-label={label}>{rows.map((row) => (
        <li key={row.id} className="kb-review-row">
          {action && <input type="checkbox" aria-label={r.select(row.title)} checked={chosen.includes(row.id)}
            onChange={(event) => setChosen((now) => (event.target.checked ? [...now, row.id] : now.filter((id) => id !== row.id)))} />}
          {row.body}
        </li>
      ))}</ul>
    </>}
  </>;
}
