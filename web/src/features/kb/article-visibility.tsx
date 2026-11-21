import { useId, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import { editable } from './article-draft';
import { kbApi, visibilitySchema } from './kb-api';
import type { KBArticle, KBVisibility } from './kb-api';
import { visibilityMessages as v } from './visibility-messages';

// "Who can see this" in the editor's side column. A saved article applies a choice at once and adds no version,
// so an edit in progress stays; raising it on a published article may be refused (approval_required, or the
// permission to publish), and the server's message says why. A new article keeps the choice until it is created.

interface ArticleVisibilityProps {
  headingId: string;
  // A saved article: the change goes to the server and onChanged gets the answer and a notice.
  article?: KBArticle;
  onChanged?: (article: KBArticle, notice: string) => void;
  // A new article: the choice is held by the editor and sent on create.
  value?: KBVisibility;
  onChoose?: (visibility: KBVisibility) => void;
}

export function ArticleVisibilityBox({ headingId, article, onChanged, value, onChoose }: ArticleVisibilityProps) {
  const name = useId();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const chosen = article?.visibility ?? value ?? 'everyone';
  const from = article?.visibility_from;
  const choose = async (visibility: KBVisibility) => {
    if (visibility === chosen || busy) return;
    setProblem(undefined);
    if (!article) { onChoose?.(visibility); return; }
    setBusy(true);
    try {
      const changed = await kbApi.updateArticle(article.id, { updated_at: editable(article).updated_at, visibility });
      onChanged?.(changed, v.done[changed.effective_visibility]);
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : v.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="kb-side-box kb-visibility-box">
      <h3 id={headingId}>{v.heading}</h3>
      <div role="radiogroup" aria-labelledby={headingId} aria-busy={busy || undefined}>
        {visibilitySchema.options.map((option) => (
          <label key={option} className="kb-visibility-option">
            <input type="radio" name={name} value={option} checked={chosen === option} onChange={() => void choose(option)} />
            <span><strong>{v.names[option]}</strong><small className="muted-copy">{v.lines[option]}</small></span>
          </label>
        ))}
        {from && <p className="kb-side-status">{v.because(from.visibility, from.name)}</p>}
      </div>
      {problem && <small role="alert" className="error-copy">{problem}</small>}
    </section>
  );
}
