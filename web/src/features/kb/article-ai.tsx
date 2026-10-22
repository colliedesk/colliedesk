import { useState } from 'react';
import { formatDay } from '../../lib/format-date';
import { Icon } from './editor/icons';
import { kbApi } from './kb-api';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';

// Whether AI features (reply drafts, triage suggestions) may read the article: a box in the editor's side
// column with the way out and back, and a note above the text while it is excluded.

interface ArticleAIProps {
  article: KBArticle;
  timezone: string;
  // Called with the article as the server answered; its text and updated_at are unchanged.
  onChanged: (article: KBArticle, notice: string) => void;
}

function useAIExclusion({ article, onChanged }: Omit<ArticleAIProps, 'timezone'>) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const change = async (exclude: boolean) => {
    if (busy) return;
    setBusy(true);
    setProblem(undefined);
    try {
      const changed = exclude ? await kbApi.excludeFromAI(article.id) : await kbApi.includeInAI(article.id);
      onChanged(changed, exclude ? kbMessages.aiExcludedDone : kbMessages.aiIncludedDone);
    } catch {
      setProblem(kbMessages.aiChangeFailed);
    } finally {
      setBusy(false);
    }
  };
  return { busy, problem, change };
}

export function ArticleAIBox({ article, timezone, onChanged, headingId }: ArticleAIProps & { headingId: string }) {
  const { busy, problem, change } = useAIExclusion({ article, onChanged });
  const exclusion = article.ai_exclusion;
  return (
    <section className="kb-side-box kb-ai-box" aria-labelledby={headingId}>
      <h3 id={headingId}><span className="kb-ai-chip" aria-hidden="true">{kbMessages.aiChip}</span>{kbMessages.usedByAI}</h3>
      <p className="kb-side-status">{exclusion
        ? kbMessages.aiExcludedSince(formatDay(exclusion.at, timezone), exclusion.by?.name)
        : article.status === 'published' ? kbMessages.aiIncluded : kbMessages.aiIncludedDraft}</p>
      {/* aria-disabled, not disabled: a disabled button drops the keyboard focus to the page. */}
      <button id={`${headingId}-button`} type="button" aria-disabled={busy || undefined} onClick={() => void change(!exclusion)}>
        <Icon name={exclusion ? 'eye' : 'eyeOff'} />{exclusion ? kbMessages.includeInAI : kbMessages.excludeFromAI}</button>
      {problem && <small role="alert" className="error-copy">{problem}</small>}
    </section>
  );
}

// ArticleAINote says above the text that AI skips the article, with the way back.
export function ArticleAINote({ article, onChanged }: Omit<ArticleAIProps, 'timezone'>) {
  const { busy, problem, change } = useAIExclusion({ article, onChanged });
  return (
    <div className="kb-ai-note" role="status">
      <Icon name="eyeOff" />
      <span><strong>{kbMessages.aiExcludedTitle}</strong> {kbMessages.aiExcludedBody}</span>
      <button type="button" aria-disabled={busy || undefined} onClick={() => void change(false)}>{kbMessages.includeInAI}</button>
      {problem && <small role="alert" className="error-copy">{problem}</small>}
    </div>
  );
}
