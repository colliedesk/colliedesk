import { useHolds } from '../../lib/permissions-context';
import type { KBArticleGapSource } from './kb-api';
import { gapsMessages as g } from './gaps-messages';

// ArticleSourcesBox names what an article drafted from a knowledge gap was written from. The links go only
// to a team member who may read conversations; the server leaves the list empty for anyone else.
export function ArticleSourcesBox({ source, headingId }: { source?: KBArticleGapSource; headingId: string }) {
  const holds = useHolds();
  if (!source) return null;
  const conversations = source.conversations;
  return (
    <section className="kb-side-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{g.sources}</h3>
      {!holds('conversations:read') ? <p className="muted-copy">{g.sourcesHidden}</p>
        : conversations.length === 0 ? <p className="muted-copy">{g.sourcesNone}</p>
          : <>
            <p className="muted-copy">{g.sourcesLead}</p>
            <ul aria-label={g.sourcesLabel}>{conversations.map((c) => (
              <li key={c.conversation_id}><a href={`/conversations/${c.conversation_id}`}>{c.display_ref} {c.subject}</a></li>
            ))}</ul>
          </>}
    </section>
  );
}
