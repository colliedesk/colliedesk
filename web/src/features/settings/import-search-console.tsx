import { useId } from 'react';
import { z } from 'zod';

// The import report's section on the Search Console tokens of the old help center (freshdesk/search-console.go).
// It lives in its own chunk, loaded only for a report that has the section, so the first bundle stays within its budget.
const sectionSchema = z.object({
  status: z.string(), google: z.string(), bing: z.string(), detail: z.array(z.string()).nullable().optional(),
});

const sentences: Record<string, string> = {
  carried: 'carried over to Settings → Help center → Search Console.',
  already_set: 'the help center already has one, so it was kept.',
  invalid: 'found, but it is not a token the help center can serve.',
};

export default function ImportSearchConsoleSummary({ section }: { section: unknown }) {
  const heading = useId();
  const parsed = sectionSchema.safeParse(section);
  if (!parsed.success) return null;
  const { status, google, bing, detail } = parsed.data;
  const say = (tool: string, state: string) => `${tool}: ${sentences[state] ?? 'no verification tag in the old help center.'}`;
  return <section className="import-links" aria-labelledby={heading}>
    <h3 id={heading} className="settings-group-label">Search Console</h3>
    <p>{say('Google', google)}</p>
    <p>{say('Bing', bing)}</p>
    {status !== 'OK' && <ul>{(detail ?? []).map((line) => <li key={line}>{line}</li>)}</ul>}
  </section>;
}
