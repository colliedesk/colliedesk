import { useId, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { reason } from './kb-tree';
import { KBHead } from './kb-ui';
import { reviewApi } from './review-api';
import type { KBReviewDetail } from './review-api';
import { reviewMessages as r } from './review-messages';

// fileChanges is the files the proposed text lists, in its order, marked against the live text's, then the
// live files it no longer lists: an approval covers the list too.
function fileChanges(review: KBReviewDetail) {
  const live = review.live?.attachments ?? [];
  const proposed = review.proposed.attachments;
  const kept = new Set(live.map((file) => file.id));
  const listed = new Set(proposed.map((file) => file.id));
  return [
    ...proposed.map((file) => ({ id: file.id, name: file.name, change: review.live && kept.has(file.id) ? 'same' : 'added' })),
    ...live.filter((file) => !listed.has(file.id)).map((file) => ({ id: file.id, name: file.name, change: 'removed' })),
  ];
}

// ReviewRoute is one review: what changed against the live text, line by line, and the reviewer's answer:
// Approve, Request changes with one line for the author, or Edit the text (which replaces this review).
export function ReviewRoute({ reviewId }: { reviewId: string }) {
  const client = useQueryClient();
  const key = ['kb-review', reviewId];
  const review = useQuery({ queryKey: key, queryFn: () => reviewApi.review(reviewId) });
  const done = (next: unknown) => {
    client.setQueryData(key, (now: KBReviewDetail | undefined) => (now ? { ...now, ...(next as object) } : now));
    void client.invalidateQueries({ queryKey: ['kb-reviews'] });
    void client.invalidateQueries({ queryKey: ['kb-articles'] });
  };
  const missing = (review.error instanceof InvalidRequestError && review.error.detail?.code === 'not_found') || review.error?.message === 'API 404';
  return (
    <main className="kb-frame kb-languages-page kb-reviews-page" aria-labelledby="kb-heading">
      <KBHead current="reviews" heading={r.heading} actions={<a className="button-link" href="/knowledge/reviews">{r.back}</a>} />
      <div className="kb-languages-scroll">
        {review.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
        {review.isError && <p role="alert" className="error-copy">{missing ? r.notFound : r.loadFailed}</p>}
        {review.data && <ReviewBody review={review.data} onDecided={done} />}
      </div>
    </main>
  );
}

function ReviewBody({ review, onDecided }: { review: KBReviewDetail; onDecided: (next: unknown) => void }) {
  const id = useId();
  const holds = useHolds();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const [asking, setAsking] = useState(false);
  const [note, setNote] = useState('');
  const [notice, setNotice] = useState('');
  const [problem, setProblem] = useState<string>();
  const approve = useCreateAction((_: undefined, key: string) => reviewApi.approve(review.id, key));
  const sendBack = useCreateAction((line: string, key: string) => reviewApi.requestChanges(review.id, line, key));
  const busy = approve.pending || sendBack.pending;
  const mine = review.reviewers.some((reviewer) => reviewer.agent_id === me.data?.agent.id);
  const decidable = review.state === 'open' && review.current && mine && holds('kb_articles:publish');
  const edit = `/knowledge/articles/${review.article_id}${review.locale ? `/translations/${encodeURIComponent(review.locale)}` : ''}`;
  const act = async (run: () => Promise<unknown>, said: string) => {
    setProblem(undefined);
    try {
      onDecided(await run());
      setAsking(false);
      setNotice(said);
    } catch (error) {
      setProblem(reason(error, r.actionFailed));
    }
  };
  const files = fileChanges(review);
  return (
    <section className="kb-languages kb-review" aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`}>{review.article_title}</h2>
      <p className="muted-copy">{review.locale && <>{r.translation(review.locale)} · </>}{r.states[review.state]} · {r.sentBy(review.requested_by?.name ?? '')}</p>
      {review.requested_by_token && <p className="kb-hint">{r.byAgent(review.requested_by_token.label)}</p>}
      {review.note && <p>{r.note}: {review.note}</p>}
      {review.decision_note && <p>{r.changesAsked(review.decided_by?.name ?? '', review.decision_note)}</p>}
      {!review.current && <p className="warning-note">{r.notCurrent}</p>}
      <p role="status" aria-live="polite" className={notice ? 'kb-review-ok' : 'sr-only'}>{notice}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <div className="button-row">
        {decidable && <button type="button" className="primary-action" disabled={busy} onClick={() => void act(() => approve.run(undefined), r.approved)}>{r.approve}</button>}
        {decidable && <button type="button" disabled={busy} aria-expanded={asking} onClick={() => setAsking(!asking)}>{r.requestChanges}</button>}
        <a className="button-link" href={edit}>{r.edit}</a>
      </div>
      {asking && <form className="field" onSubmit={(event) => { event.preventDefault(); if (note.trim()) void act(() => sendBack.run(note.trim()), r.changesSent); }}>
        <label htmlFor={`${id}-changes`}>{r.changesNote}</label>
        <input id={`${id}-changes`} value={note} required maxLength={500} placeholder={r.changesPlaceholder} onChange={(event) => setNote(event.target.value)} />
        <div className="button-row"><button type="submit" disabled={busy || !note.trim()}>{r.sendChanges}</button></div>
      </form>}
      <h3>{r.changes}</h3>
      {!review.live && <p className="muted-copy">{r.notLive}</p>}
      {review.live && review.diff.every((line) => line.change === 'same') && <p className="muted-copy">{r.noTextChange}</p>}
      <ul className="kb-diff" aria-label={r.changes}>{review.diff.map((line, at) => (
        <li key={at} data-change={line.change}>{line.change !== 'same' && <span className="sr-only">{line.change === 'removed' ? r.removed : r.added}</span>}{line.text}</li>
      ))}</ul>
      {files.length > 0 && <>
        <h3>{r.files}</h3>
        <ul className="kb-diff" aria-label={r.files}>{files.map((file) => (
          <li key={file.id} data-change={file.change}>{file.change !== 'same' && <span className="sr-only">{file.change === 'removed' ? r.removed : r.added}</span>}{file.name}</li>
        ))}</ul>
      </>}
    </section>
  );
}
