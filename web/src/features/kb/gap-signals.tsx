import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { ArticleLinkPicker } from './article-link-picker';
import type { KBArticleSummary } from './kb-api';
import { kbMessages } from './kb-messages';
import { gapsApi } from './gaps-api';
import type { KBGap } from './gaps-api';
import { gapsMessages as g } from './gaps-messages';

interface GapSignalsProps {
  gap: KBGap;
  timeZone: string;
  // change is offered to a role that may edit articles; it is undefined for one that only reads.
  change?: {
    fix: (article: KBArticleSummary) => void;
    dismiss: () => void;
    reopen: () => void;
    busy: boolean;
    canManage: boolean;
  };
  // draft is offered to a role that may create articles: the AI writes the missing article as a draft.
  draft?: { run: () => void; busy: boolean };
}

// GapSignals is an opened gap: what each reader searched, wrote or asked, newest first, and what to do
// with it. Nothing tells the askers apart.
export function GapSignals({ gap, timeZone, change, draft }: GapSignalsProps) {
  const [picking, setPicking] = useState(false);
  const detail = useQuery({ queryKey: ['kb-gaps', 'detail', gap.id], queryFn: () => gapsApi.get(gap.id) });
  const data = detail.data;
  return (
    <div className="kb-gap-signals">
      {detail.isError && <p role="alert" className="error-copy">{g.signalsFailed}{' '}
        <button type="button" className="link-action" onClick={() => void detail.refetch()}>{kbMessages.retry}</button></p>}
      {detail.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
      {data && <>
        <ul className="kb-gap-signal-list" aria-label={g.asked(gap.title)}>{data.signals.map((signal, at) => (
          <li key={at}>
            <span className="kb-gap-kind">{g.kinds[signal.kind]}</span>
            <span className="kb-gap-text">{signal.text}</span>
            <time className="muted-copy" dateTime={signal.created_at}>{formatDate(signal.created_at, timeZone)}</time>
          </li>
        ))}</ul>
        {data.signal_count > data.signals.length && <p className="muted-copy">{g.more(data.signals.length, data.signal_count)}</p>}
      </>}
      {gap.fixed_by_article && <p className="muted-copy">{g.fixedBy} <a href={`/knowledge/articles/${gap.fixed_by_article.id}`}>{gap.fixed_by_article.title}</a>
        {gap.fixed_automatically && <> {g.fixedAutomatically}</>}</p>}
      {(change || draft) && <div className="kb-template-actions">
        {gap.status === 'open' && draft && <button type="button" className="primary-action" title={g.draftTitle} disabled={draft.busy}
          onClick={draft.run}>{draft.busy ? g.drafting : g.draft}</button>}
        {gap.status === 'open' && change && <>
          <button type="button" disabled={change.busy} onClick={() => setPicking(true)}>{g.link}</button>
          <button type="button" disabled={change.busy} onClick={change.dismiss}>{g.dismiss}</button>
        </>}
        {gap.status !== 'open' && change && <button type="button" disabled={change.busy} onClick={change.reopen}>{g.reopen}</button>}
      </div>}
      {picking && change && <ArticleLinkPicker canManage={change.canManage}
        onPick={(article) => { setPicking(false); change.fix(article); }}
        onClose={() => setPicking(false)} />}
    </div>
  );
}
