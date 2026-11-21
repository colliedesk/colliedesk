import { useId, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { useHolds } from '../../lib/permissions-context';
import { editable } from './article-draft';
import { kbApi } from './kb-api';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';

// The article's author in the editor's side column. A change applies at once and adds an author_change version,
// so an edit in progress stays to be saved. Only an active teammate can be chosen; an author who left still shows.

const authorMessages = {
  heading: 'Author',
  changed: (name: string) => `Author changed to ${name}`,
  failed: 'The author was not changed. Try again.',
};

interface ArticleAuthorProps {
  headingId: string;
  article: KBArticle;
  onChanged: (article: KBArticle, notice: string) => void;
}

export function ArticleAuthorBox({ headingId, article, onChanged }: ArticleAuthorProps) {
  const id = useId();
  const holds = useHolds();
  const canChange = holds('team:read') && holds('kb_articles:update');
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: canChange });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const current = article.author_agent_id ?? '';
  const people = (team.data?.items ?? []).filter((agent) => !agent.deactivated_at);
  const listed = people.some((agent) => agent.id === current);
  const choose = async (agentId: string) => {
    if (!agentId || agentId === current || busy) return;
    setProblem(undefined);
    setBusy(true);
    try {
      const changed = await kbApi.updateArticle(article.id, { updated_at: editable(article).updated_at, author_agent_id: agentId });
      onChanged(changed, authorMessages.changed(kbMessages.authorName(changed.author)));
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : authorMessages.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="kb-side-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{authorMessages.heading}</h3>
      {canChange && team.isSuccess
        ? <select id={`${id}-author`} aria-labelledby={headingId} value={current} disabled={busy} onChange={(event) => void choose(event.currentTarget.value)}>
          {!listed && <option value={current}>{kbMessages.authorName(article.author)}</option>}
          {people.map((agent) => <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}</option>)}
        </select>
        : <p>{kbMessages.authorName(article.author)}</p>}
      {problem && <small role="alert" className="error-copy">{problem}</small>}
    </section>
  );
}
