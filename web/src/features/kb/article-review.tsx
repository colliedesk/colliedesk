import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { useHolds } from '../../lib/permissions-context';
import { approvalState, requiresApproval } from './approval-state';
import type { Approval } from './approval-state';
import type { KBApprovalMode } from './kb-api';
import { kbApi } from './kb-api';
import { reason } from './kb-tree';
import { openForMeKey, reviewApi, reviewsKey } from './review-api';
import type { KBReview } from './review-api';
import { reviewMessages as r } from './review-messages';
import { ReviewSendDialog } from './review-send-dialog';

export interface TextApproval extends Approval<KBReview> { loading: boolean }

// useTextApproval: whether a text (an article's own, or one translation's) may be published now. collectionIds
// are every collection it is in, was in or moves to; version changes with each save, so the reviews load again
// (a save closes an open review and the server opens one of the new text).
export function useTextApproval({ articleId, locale, collections, collectionIds, dirty, version }: {
  articleId?: string; locale?: string; collections: { id: string; require_approval: KBApprovalMode }[]; collectionIds: string[]; dirty: boolean; version?: string;
}): TextApproval {
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter });
  const required = requiresApproval(collections, center.data?.require_approval ?? false, collectionIds.filter(Boolean));
  const reviews = useQuery({ queryKey: [...reviewsKey(articleId ?? '', locale), version ?? ''], queryFn: () => reviewApi.ofText(articleId!, locale), enabled: required && articleId !== undefined });
  return { ...approvalState({ required, reviews: reviews.data?.reviews ?? [], dirty }), loading: required && articleId !== undefined && reviews.isPending };
}

// ArticleReviewBox says where the text is on its way to the help center and offers the next step: send it
// for review, wait (or withdraw), fix what the reviewer asked, or publish.
export function ArticleReviewBox({ articleId, locale, approval, dirty, saved, headingId, reasonId }: {
  articleId?: string; locale?: string; approval: TextApproval; dirty: boolean; saved: boolean; headingId: string; reasonId: string;
}) {
  const client = useQueryClient();
  const holds = useHolds();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const [sending, setSending] = useState(false);
  const [notice, setNotice] = useState('');
  const [problem, setProblem] = useState<string>();
  const withdraw = useCreateAction((id: string, key: string) => reviewApi.withdraw(id, key));
  if (!approval.required) return null;
  const { open, approved } = approval;
  const canSend = articleId !== undefined && saved && !dirty && !open && !approved && holds('kb_articles:update');
  const pull = async (review: KBReview) => {
    setProblem(undefined);
    try {
      await withdraw.run(review.id);
      setNotice(r.withdrawn);
      await client.invalidateQueries({ queryKey: reviewsKey(review.article_id, locale) });
      void client.invalidateQueries({ queryKey: openForMeKey });
    } catch (error) {
      setProblem(reason(error, r.actionFailed));
    }
  };
  return (
    <section className="kb-side-box kb-review-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{r.review}</h3>
      <p className="sr-only" role="status" aria-live="polite">{notice}</p>
      {approval.loading ? <p className="kb-hint">…</p>
        : <p id={reasonId} className={approved && !approval.blocked ? 'kb-review-ok' : 'kb-hint'}>{approval.blocked ?? r.approvedBy(approved?.decided_by?.name ?? '')}</p>}
      {open && open.note && <small className="kb-hint">{r.note}: {open.note}</small>}
      {open && <a className="button-link" href={`/knowledge/reviews/${open.id}`}>{r.review}</a>}
      <div className="button-row">
        {!open && !(approved && !dirty) && <button type="button" disabled={!canSend} aria-describedby={dirty || !saved ? `${reasonId}-save` : undefined}
          onClick={() => setSending(true)}>{r.sendForReview}</button>}
        {open && <button type="button" disabled={withdraw.pending} onClick={() => void pull(open)}>{r.withdraw}</button>}
      </div>
      {(dirty || !saved) && !open && <small id={`${reasonId}-save`} className="kb-hint">{r.saveFirst}</small>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {sending && articleId && <ReviewSendDialog articleId={articleId} locale={locale} meId={me.data?.agent.id}
        onSent={() => { setSending(false); setNotice(r.sent); }} onClose={() => setSending(false)} />}
    </section>
  );
}
