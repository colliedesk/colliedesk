import { useId, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { reason } from './kb-tree';
import { KBDialog } from './kb-ui';
import { openForMeKey, reviewApi, reviewsKey } from './review-api';
import type { KBReview } from './review-api';
import { reviewMessages as r } from './review-messages';

export interface Candidate { id: string; name: string }

// useReviewers lists who may review: active teammates whose roles may publish articles, never the person asking
// (the author does not approve their own text). Without team:read the roles cannot be read; the team list
// stands in, and the server refuses anyone who may not publish.
export function useReviewers(meId: string | undefined) {
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.roles(), retry: false, staleTime: 60_000 });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: roles.isError, staleTime: 60_000 });
  const byId = new Map<string, Candidate>();
  if (roles.data) {
    for (const role of roles.data.items) {
      if (!role.permissions.includes('kb_articles:publish')) continue;
      for (const member of role.members) if (member.active && member.id !== meId) byId.set(member.id, { id: member.id, name: member.name });
    }
  } else if (team.data) {
    for (const member of team.data.items) if (!member.deactivated_at && member.id !== meId) byId.set(member.id, { id: member.id, name: member.display_name || member.email });
  }
  return { reviewers: [...byId.values()].sort((a, b) => a.name.localeCompare(b.name)), loading: roles.isPending || (roles.isError && team.isPending) };
}

// ReviewSendDialog sends the saved text of an article (or of one translation) to the chosen reviewers.
export function ReviewSendDialog({ articleId, locale, meId, onSent, onClose }: { articleId: string; locale?: string; meId?: string; onSent: (review: KBReview) => void; onClose: () => void }) {
  const id = useId();
  const client = useQueryClient();
  const { reviewers, loading } = useReviewers(meId);
  const [chosen, setChosen] = useState<string[]>([]);
  const [note, setNote] = useState('');
  const [problem, setProblem] = useState<string>();
  const send = useCreateAction((of: { reviewerIds: string[]; note: string }, key: string) => reviewApi.send({ articleId, locale, ...of }, key));
  const busy = send.pending;
  const submit = async () => {
    if (chosen.length === 0) { setProblem(r.pickReviewer); return; }
    setProblem(undefined);
    try {
      const review = await send.run({ reviewerIds: chosen, note: note.trim() });
      await client.invalidateQueries({ queryKey: reviewsKey(articleId, locale) });
      void client.invalidateQueries({ queryKey: openForMeKey });
      onSent(review);
    } catch (error) {
      setProblem(reason(error, r.actionFailed));
    }
  };
  const toggle = (reviewer: string, on: boolean) => setChosen((now) => (on ? [...now, reviewer] : now.filter((each) => each !== reviewer)));
  return (
    <KBDialog title={r.sendHeading} describedBy={`${id}-help`} onClose={onClose} footer={<>
      <button type="button" disabled={busy} onClick={onClose}>{r.cancel}</button>
      <button type="button" className="primary-action" disabled={busy || reviewers.length === 0} onClick={() => void submit()}>{busy ? r.sending : r.sendForReview}</button>
    </>}>
      <p id={`${id}-help`} className="muted-copy">{r.sendHelp}</p>
      <fieldset className="kb-reviewer-list">
        <legend>{r.reviewers}</legend>
        {loading ? <p className="kb-hint" role="status">…</p>
          : reviewers.length === 0 ? <p className="kb-hint">{r.noReviewers}</p>
            : reviewers.map((reviewer) => (
              <label key={reviewer.id} className="check-row"><input type="checkbox" checked={chosen.includes(reviewer.id)}
                onChange={(event) => toggle(reviewer.id, event.target.checked)} /><span>{reviewer.name}</span></label>
            ))}
      </fieldset>
      <div className="field"><label htmlFor={`${id}-note`}>{r.note}</label>
        <input id={`${id}-note`} value={note} maxLength={500} placeholder={r.notePlaceholder} onChange={(event) => setNote(event.target.value)} /></div>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
    </KBDialog>
  );
}
