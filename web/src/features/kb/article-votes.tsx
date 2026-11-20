import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useCreateMutation } from '../../api/use-create';
import { formatDay } from '../../lib/format-date';
import { useHolds } from '../../lib/permissions-context';
import { KBDialog } from './kb-ui';
import { votesApi, type KBArticleVotes } from './votes-api';
import { votesMessages as m } from './votes-messages';

// ArticleVotesBox shows what readers answered to "Was this article helpful?" on the public page: the count
// the page shows, each reader's vote with what they said was missing, and the counts earlier resets kept.

const shown = 5;

interface ArticleVotesProps { articleId: string; title: string; timezone: string; headingId: string }

export function ArticleVotesBox({ articleId, title, timezone, headingId }: ArticleVotesProps) {
  const holds = useHolds();
  const client = useQueryClient();
  const queryKey = ['kb-article-votes', articleId];
  const votes = useQuery({ queryKey, queryFn: () => votesApi.votes(articleId) });
  const [asking, setAsking] = useState(false);
  const [all, setAll] = useState(false);
  // One key per press: a double press or a retry after a failure resets once.
  const reset = useCreateMutation<KBArticleVotes>({
    create: (_none, key) => votesApi.reset(articleId, key),
    onSuccess: (answer) => { client.setQueryData(queryKey, answer); setAsking(false); },
  });
  const data = votes.data;
  const total = data ? data.helpful + data.not_helpful : 0;
  const rows = data ? (all ? data.votes : data.votes.slice(0, shown)) : [];
  return (
    <section className="kb-side-box kb-votes-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{m.heading}</h3>
      {votes.isError && <small role="alert" className="error-copy">{m.loadFailed}</small>}
      {votes.isPending && <p className="kb-hint">{m.loading}</p>}
      {data && <>
        <p className="kb-side-status">{total > 0 ? m.count(data.helpful, total) : m.none}</p>
        {data.imported.helpful + data.imported.not_helpful > 0 &&
          <p className="kb-hint">{m.imported(data.imported.helpful, data.imported.helpful + data.imported.not_helpful)}</p>}
        {rows.length > 0 && <ul className="kb-vote-list" aria-label={m.readers}>
          {rows.map((vote, at) => (
            <li key={`${vote.updated_at}-${at}`}>
              <span className="kb-vote-line">
                <span className="kb-vote-answer" data-helpful={vote.helpful}>{vote.helpful ? m.yes : m.no}</span>
                <span className="kb-hint">{formatDay(vote.updated_at, timezone)}</span>
              </span>
              {vote.missing && <q className="kb-vote-missing">{vote.missing}</q>}
            </li>
          ))}
        </ul>}
        {!all && data.votes.length > shown && <button type="button" className="link-button" onClick={() => setAll(true)}>{m.showAll(data.votes.length)}</button>}
        {holds('kb_articles:update') && total > 0 && <button type="button" onClick={() => setAsking(true)}>{m.reset}</button>}
        {data.resets.length > 0 && <div className="kb-vote-history">
          <h4>{m.history}</h4>
          <ul>{data.resets.map((earlier) => (
            <li key={earlier.id} className="kb-hint">
              {m.earlier(formatDay(earlier.created_at, timezone), earlier.helpful, earlier.helpful + earlier.not_helpful, earlier.reset_by?.name ?? earlier.token?.label)}</li>
          ))}</ul>
        </div>}
      </>}
      {asking && <KBDialog title={m.resetHeading(title)} role="alertdialog" onClose={() => setAsking(false)} footer={<>
        <button type="button" disabled={reset.isPending} onClick={() => setAsking(false)}>{m.cancel}</button>
        <button type="button" className="danger-solid" disabled={reset.isPending} onClick={() => reset.mutate()}>{m.reset}</button>
      </>}>
        <p className="muted-copy" data-quiet="consequence">{m.resetBody}</p>
        {reset.isError && <small role="alert" className="error-copy">{m.resetFailed}</small>}
      </KBDialog>}
    </section>
  );
}
