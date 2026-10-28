import { z } from 'zod';
import { BookIcon, InboxIcon } from '../../ui/icons';
import './ai-sources.css';

// The articles and solved conversations an AI draft or triage suggestion read, named from the database.
// Shared by both lazy parts; neither is in the first bundle.
export const aiSourceSchema = z.object({ kind: z.enum(['kb_article', 'conversation']), id: z.string().uuid(), locale: z.string().optional(), number: z.number().int().optional(), title: z.string() });
export type AISource = z.infer<typeof aiSourceSchema>;

const text = { sources: 'Sources', article: 'Article', ticket: 'Solved ticket' };

// AISources lists the sources on one line that scrolls sideways; empty says what was used instead.
export function AISources({ sources, empty }: { sources: AISource[]; empty?: string }) {
  if (sources.length === 0 && !empty) return null;
  return (
    <div className="ai-sources">
      <span>{text.sources}</span>
      {sources.length === 0 ? <span className="ai-sources-none">{empty}</span> : (
        <ul>{sources.map((source) => (
          <li key={source.kind + source.id}>
            <a href={source.kind === 'kb_article' ? `/knowledge/articles/${source.id}` : `/conversations/${source.id}`} target="_blank" rel="noopener">
              {source.kind === 'kb_article' ? <BookIcon /> : <InboxIcon />}
              <span className="sr-only">{source.kind === 'kb_article' ? text.article : text.ticket}: </span>{source.title}
            </a>
          </li>
        ))}</ul>
      )}
    </div>
  );
}
