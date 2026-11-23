import { useHolds } from '../../lib/permissions-context';
import type { KBArticleGapSource, KBArticleReplySource } from './kb-api';
import { gapsMessages as g } from './gaps-messages';

interface ArticleSourcesProps { source?: KBArticleGapSource; reply?: KBArticleReplySource; headingId: string }

// ArticleSourcesBox names what an article was started from: the conversations that asked for it or that an AI
// draft read, or the sent reply it keeps with the question it answered. The links go only to a team member who
// may read conversations; the server leaves them out for anyone else.
export function ArticleSourcesBox({ source, reply, headingId }: ArticleSourcesProps) {
  const holds = useHolds();
  if (!source && !reply) return null;
  const conversations = source?.conversations ?? [];
  return (
    <section className="kb-side-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{g.sources}</h3>
      {reply && <>
        <p className="muted-copy">{g.replyLead}</p>
        <p><a href={`/conversations/${reply.conversation_id}`}>{reply.display_ref} {reply.subject}</a></p>
        {reply.question && <><p className="muted-copy">{g.replyQuestion}</p><blockquote className="kb-source-question">{reply.question}</blockquote></>}
      </>}
      {source && (!holds('conversations:read') ? <p className="muted-copy">{source.requested ? g.sourcesRequestedHidden : g.sourcesHidden}</p>
        : conversations.length === 0 ? (source.requested ? null : <p className="muted-copy">{g.sourcesNone}</p>)
          : <>
            <p className="muted-copy">{source.requested ? g.sourcesRequested : g.sourcesLead}</p>
            <ul aria-label={g.sourcesLabel}>{conversations.map((c) => (
              <li key={c.conversation_id}><a href={`/conversations/${c.conversation_id}`}>{c.display_ref} {c.subject}</a></li>
            ))}</ul>
          </>)}
    </section>
  );
}
