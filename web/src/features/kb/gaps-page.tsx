import { useId, useState } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { formatDate } from '../../lib/format-date';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { GapSignals } from './gap-signals';
import { gapKinds, gapsApi } from './gaps-api';
import type { KBGap, KBGapKind, KBGapStatus } from './gaps-api';
import { gapsMessages as g } from './gaps-messages';
import { kbMessages } from './kb-messages';
import { KBHead, KBToast } from './kb-ui';

const statuses: KBGapStatus[] = ['open', 'fixed', 'dismissed'];
type Change = RequestBody<'updateKBGap'>;
interface Toast { text: string; undo?: { gap: KBGap; body: Change } }

// GapsRoute ranks what readers asked for and did not find: the gaps with the most askers in 30 days come
// first. A row opens its signals; a gap is linked to the article that answers it, or dismissed.
export function GapsRoute() {
  const holds = useHolds();
  const id = useId();
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timeZone = me.data?.timezone ?? 'UTC';
  const [status, setStatus] = useState<KBGapStatus>('open');
  const [kind, setKind] = useState<KBGapKind | ''>('');
  const [opened, setOpened] = useState<string>();
  const [toast, setToast] = useState<Toast>();
  const gaps = useQuery({ queryKey: ['kb-gaps', status, kind], queryFn: ({ signal }) => gapsApi.list(status, kind || undefined, signal), placeholderData: keepPreviousData });
  const update = useMutation({
    mutationFn: ({ gap, body }: { gap: KBGap; body: Change; toast: (saved: KBGap) => Toast | undefined }) => gapsApi.update(gap.id, body),
    onSuccess: (saved, { toast: next }) => { setToast(next(saved)); setOpened(undefined); },
    onError: () => setToast({ text: g.saveFailed }),
    onSettled: () => client.invalidateQueries({ queryKey: ['kb-gaps'] }),
  });
  const change = (gap: KBGap) => holds('kb_articles:update') ? {
    busy: update.isPending, canManage: holds('help_center:update'),
    fix: (article: { id: string; title: string }) => update.mutate({ gap, body: { fixed_by_article_id: article.id }, toast: () => ({ text: g.fixed(gap.title, article.title), undo: { gap, body: { status: 'open' } } }) }),
    dismiss: () => update.mutate({ gap, body: { status: 'dismissed' }, toast: () => ({ text: g.dismissed(gap.title), undo: { gap, body: { status: 'open' } } }) }),
    reopen: () => update.mutate({ gap, body: { status: 'open' }, toast: () => ({ text: g.reopened(gap.title) }) }),
  } : undefined;
  const items = gaps.data?.items;
  return (
    <main className="kb-frame kb-languages-page kb-templates-page" aria-labelledby="kb-heading">
      <KBHead current="gaps" heading={kbMessages.heading} />
      <div className="kb-languages-scroll">
        <section className="kb-languages kb-templates" aria-labelledby="kb-gaps-heading">
          <h2 id="kb-gaps-heading">{g.heading}</h2>
          <p className="muted-copy">{g.lead} <HelpTip label={g.heading} text={g.tip} /></p>
          <div className="kb-gap-filters">
            <label htmlFor={`${id}-status`}>{g.status}</label>
            <select id={`${id}-status`} value={status} onChange={(event) => { setStatus(event.target.value as KBGapStatus); setOpened(undefined); }}>
              {statuses.map((value) => <option key={value} value={value}>{g.statuses[value]}</option>)}
            </select>
            <label htmlFor={`${id}-kind`}>{g.kind}</label>
            <select id={`${id}-kind`} value={kind} onChange={(event) => { setKind(event.target.value as KBGapKind | ''); setOpened(undefined); }}>
              <option value="">{g.allKinds}</option>
              {gapKinds.map((value) => <option key={value} value={value}>{g.kinds[value]}</option>)}
            </select>
          </div>
          {gaps.isError && <p role="alert" className="error-copy">{g.loadFailed}{' '}
            <button type="button" className="link-action" onClick={() => void gaps.refetch()}>{kbMessages.retry}</button></p>}
          {gaps.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
          {items && items.length === 0 && <p className="empty-copy">{g.empty[status]}</p>}
          {items && items.length > 0 && <ul className="kb-template-list kb-gap-list" aria-label={g.listLabel(status)}>{items.map((gap) => (
            <li key={gap.id}>
              <div className="kb-template-text">
                <button type="button" className="link-action kb-gap-title" aria-expanded={opened === gap.id}
                  onClick={() => setOpened(opened === gap.id ? undefined : gap.id)}>{gap.title}</button>
                <small className="muted-copy">{g.stats(gap.recent_askers, gap.askers, formatDate(gap.last_seen, timeZone))}</small>
                <ul className="kb-gap-kinds" aria-label={g.kindsLabel}>{gap.kinds.map((value) => <li key={value}>{g.kinds[value]}</li>)}</ul>
                {opened === gap.id && <GapSignals gap={gap} timeZone={timeZone} change={change(gap)} />}
              </div>
            </li>
          ))}</ul>}
        </section>
      </div>
      {toast && <KBToast text={toast.text} undo={toast.undo && g.undo} onDismiss={() => setToast(undefined)}
        onUndo={() => { const undo = toast.undo; if (undo) update.mutate({ ...undo, toast: () => undefined }); setToast(undefined); }} />}
    </main>
  );
}
