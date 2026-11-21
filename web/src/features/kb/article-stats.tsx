import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { articleStats } from '../../api/kb-article-stats';

// ArticleStatsBox shows how much the article did in the last 30 days: the readers who opened it on the help
// center and the sent replies that linked it or used it as an AI source, each opening its conversation.

const m = {
  heading: 'Last 30 days',
  loading: 'Loading…',
  loadFailed: 'Could not load the views and replies.',
  views: (n: number) => (n === 0 ? 'No views on the help center' : `${n} ${n === 1 ? 'view' : 'views'} on the help center`),
  uses: (n: number) => (n === 0 ? 'Not used in a reply' : `Used in ${n} ${n === 1 ? 'reply' : 'replies'}`),
  replies: 'Replies that used this article',
  more: (shown: number, total: number) => `The newest ${shown} of ${total} replies.`,
};

type ConversationLink = (id: string, children: ReactNode) => ReactNode;
const openConversation: ConversationLink = (conversationId, children) => <Link to="/conversations/$conversationId" params={{ conversationId }}>{children}</Link>;

interface ArticleStatsProps { articleId: string; headingId: string; conversationLink?: ConversationLink }

export function ArticleStatsBox({ articleId, headingId, conversationLink = openConversation }: ArticleStatsProps) {
  const stats = useQuery({ queryKey: ['kb-article-stats', articleId], queryFn: () => articleStats(articleId) });
  const data = stats.data;
  return (
    <section className="kb-side-box kb-stats-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{m.heading}</h3>
      {stats.isError && <small role="alert" className="error-copy">{m.loadFailed}</small>}
      {stats.isPending && <p className="kb-hint">{m.loading}</p>}
      {data && <>
        <p className="kb-side-status">{m.views(data.views)}</p>
        <p className="kb-side-status">{m.uses(data.uses)}</p>
        {data.replies.length > 0 && <ul className="kb-stats-replies" aria-label={m.replies}>
          {data.replies.map((reply) => <li key={reply.message_id}>{conversationLink(reply.conversation_id, <>{reply.display_ref} {reply.subject}</>)}</li>)}
        </ul>}
        {data.replies_total > data.replies.length && <p className="kb-hint">{m.more(data.replies.length, data.replies_total)}</p>}
      </>}
    </section>
  );
}
