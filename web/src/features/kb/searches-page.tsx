import { useQuery } from '@tanstack/react-query';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { kbMessages } from './kb-messages';
import { KBHead } from './kb-ui';
import { searchesApi } from './searches-api';
import type { KBSearchTerm } from './searches-api';
import { searchesMessages as s } from './searches-messages';

// SearchesRoute shows what readers searched for on the help center in the last 7 days. The words that found
// no article come first: each is an article to write. Then the words whose results nobody opened, then the top.
export function SearchesRoute() {
  const holds = useHolds();
  const report = useQuery({ queryKey: ['kb-search-report'], queryFn: searchesApi.report, staleTime: 30_000 });
  const data = report.data;
  return (
    <main className="kb-frame kb-languages-page kb-templates-page" aria-labelledby="kb-heading">
      <KBHead current="searches" heading={kbMessages.heading} />
      <div className="kb-languages-scroll">
        <section className="kb-languages kb-templates" aria-labelledby="kb-searches-heading">
          <h2 id="kb-searches-heading">{s.heading}</h2>
          {report.isError && <p role="alert" className="error-copy">{s.loadFailed}{' '}
            <button type="button" className="link-action" onClick={() => void report.refetch()}>{kbMessages.retry}</button></p>}
          {report.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
          {data && data.searches === 0 && <p className="empty-copy">{s.empty}</p>}
          {data && data.searches > 0 && <>
            <p className="muted-copy">{s.total(data.searches)} <HelpTip label={s.heading} text={s.tip} /></p>
            <Terms label={s.noResults} lead={s.noResultsLead} empty={s.noResultsEmpty} terms={data.no_results} stats={(term) => s.stats(term.searches, null, null)}
              write={holds('kb_articles:create')} />
            <Terms label={s.nothingOpened} lead={s.nothingOpenedLead} empty={s.nothingOpenedEmpty} terms={data.nothing_opened} stats={(term) => s.stats(term.searches, term.results, null)} />
            <Terms label={s.top} lead={s.topLead} terms={data.top} stats={(term) => s.stats(term.searches, term.results, term.opened)} />
          </>}
        </section>
      </div>
    </main>
  );
}

interface TermsProps {
  label: string;
  lead: string;
  empty?: string;
  terms: KBSearchTerm[];
  stats: (term: KBSearchTerm) => string;
  // write links each row to a new article, for a role that may create one.
  write?: boolean;
}

function Terms({ label, lead, empty, terms, stats, write }: TermsProps) {
  return <>
    <h3>{label}</h3>
    <p className="muted-copy">{lead}</p>
    {terms.length === 0 && empty && <p className="empty-copy">{empty}</p>}
    {terms.length > 0 && <ul className="kb-template-list" aria-label={label}>{terms.map((term) => (
      <li key={term.query}>
        <div className="kb-template-text">
          <strong>{term.query}</strong>
          <small className="muted-copy">{stats(term)}</small>
        </div>
        {write && <div className="kb-template-actions">
          <a className="button-link" href="/knowledge/new" aria-label={s.write(term.query)}>{s.writeShort}</a>
        </div>}
      </li>
    ))}</ul>}
  </>;
}
