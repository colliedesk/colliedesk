import { useId } from 'react';
import type { ImportLinks } from '../../api/imports';
import { importMessages as m } from './import-messages';

// ImportLinksSummary says where the old Freshdesk help center's addresses and the Freshdesk links in
// imported articles and saved replies lead now; the downloaded report lists each one left behind.
export function ImportLinksSummary({ links }: { links: ImportLinks }) {
  const heading = useId();
  const ok = links.status === 'OK';
  return <section className="import-links" aria-labelledby={heading}>
    <h3 id={heading} className="settings-group-label">{m.linksHeading}</h3>
    <p className="import-status" data-status={links.status}>{ok ? m.linksOK : m.linksUnverified}</p>
    <p>{m.linksRedirected(links.old_addresses.redirected, links.old_addresses.found, links.help_center)}</p>
    <p>{m.linksRewritten(links.rewritten.links, links.rewritten.articles, links.rewritten.translations ?? 0, links.rewritten.saved_replies)}</p>
    {!ok && <ul>{(links.detail ?? []).map((line) => <li key={line}>{line}</li>)}</ul>}
    {!ok && <p>{m.linksReportNote}</p>}
  </section>;
}
