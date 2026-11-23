import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useCreateMutation } from '../../api/use-create';
import { formatDay } from '../../lib/format-date';
import { useHolds } from '../../lib/permissions-context';
import { conflictsApi, type KBArticleConflicts } from './conflicts-api';
import { conflictsMessages as m } from './conflicts-messages';

// ArticleConflictsBox shows the sent replies the small model found to contradict the article: what each
// said, who sent it, the model's reason and, for a member who may read it, its conversation. An editor
// clears the flag once the article is fixed or the replies were wrong.

type ConversationLink = (id: string, children: ReactNode) => ReactNode;
const openConversation: ConversationLink = (conversationId, children) => <Link to="/conversations/$conversationId" params={{ conversationId }}>{children}</Link>;

interface ArticleConflictsProps { articleId: string; timezone: string; headingId: string; conversationLink?: ConversationLink }

export function ArticleConflictsBox({ articleId, timezone, headingId, conversationLink = openConversation }: ArticleConflictsProps) {
  const holds = useHolds();
  const client = useQueryClient();
  const queryKey = ['kb-article-conflicts', articleId];
  const conflicts = useQuery({ queryKey, queryFn: () => conflictsApi.conflicts(articleId) });
  // One key per press: a double press or a retry after a failure clears once.
  const clear = useCreateMutation<KBArticleConflicts>({
    create: (_none, key) => conflictsApi.clear(articleId, key),
    onSuccess: (answer) => { client.setQueryData(queryKey, answer); },
  });
  const data = conflicts.data;
  const open = data ? data.open > 0 : true;
  // The newest clear: rows are ordered by when the model found them, not by when they were cleared.
  const last = data?.conflicts.reduce<KBArticleConflicts['conflicts'][number] | undefined>(
    (newest, row) => (row.cleared_at && (!newest?.cleared_at || row.cleared_at > newest.cleared_at) ? row : newest), undefined);
  return (
    <section className="kb-side-box kb-conflicts-box" data-open={open ? 'true' : undefined} aria-labelledby={headingId}>
      <h3 id={headingId}>{open ? m.open : m.cleared}</h3>
      {conflicts.isError && <small role="alert" className="error-copy">{m.loadFailed}</small>}
      {conflicts.isPending && <p className="kb-hint">{m.loading}</p>}
      {data && <>
        {open
          ? <><p className="kb-side-status">{m.count(data.open)}</p><p className="kb-hint">{m.next}</p></>
          : last?.cleared_at && <p className="kb-side-status">{m.clearedBy(last.cleared_by?.name ?? last.cleared_with_token?.label, formatDay(last.cleared_at, timezone))}</p>}
        {data.conflicts.length > 0 && <ul className="kb-conflict-list" aria-label={m.replies}>
          {data.conflicts.map((row) => (
            <li key={row.id} data-cleared={row.cleared_at ? 'true' : undefined}>
              <q className="kb-conflict-reply">{row.reply.excerpt}</q>
              <span className="kb-hint">{m.sentBy(row.reply.author?.name, formatDay(row.reply.sent_at, timezone))}</span>
              <span className="kb-conflict-reason">{row.reason}</span>
              {row.reply.conversation && conversationLink(row.reply.conversation.id,
                <>{row.reply.conversation.display_ref} {row.reply.conversation.subject}</>)}
            </li>
          ))}
        </ul>}
        {open && holds('kb_articles:update') && <button type="button" disabled={clear.isPending} onClick={() => clear.mutate()}>{m.clear}</button>}
        {clear.isError && <small role="alert" className="error-copy">{m.clearFailed}</small>}
      </>}
    </section>
  );
}
