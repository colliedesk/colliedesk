import type { ReactNode } from 'react';
import { formatShortDate } from '../../lib/format-date';
import type { Fix, FixConnector, FixGroup, Repo } from './autofix-api';
import { StateLabel } from './autofix-block';
import { autofixMessages as m } from './autofix-messages';
import { fixView, minutesIn, safeHref, shortId, waitsForAssignment } from './fix-view';

// age is how long a fix has been in its state, in the largest unit that fits.
export function ageOf(fix: Fix, now: number): string {
  const minutes = minutesIn(fix, now);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  return hours < 48 ? `${hours} h` : `${Math.floor(hours / 24)} d`;
}

// whyOf is the second line of a row's state: the one fact that says what it waits for.
export function whyOf(fix: Fix, timezone: string, now: number): string {
  const w = m.fixes.why;
  switch (fix.state) {
    case 'sending': return w.sending;
    case 'sent':
      if (waitsForAssignment(fix)) return fix.issue_number === null ? '' : w.issue(fix.issue_number);
      return fix.stale ? w.checkSetup(fix.connector) : fix.issue_number === null ? '' : w.issue(fix.issue_number);
    case 'working': return fix.issue_number === null ? '' : w.issue(fix.issue_number);
    case 'pr_open': return w.review;
    case 'merged': return w.notReleased;
    case 'deployed': return formatShortDate(fix.deployed_at ?? fix.state_changed_at, timezone, new Date(now));
    case 'failed': return fix.outcome_reason === 'pr_closed' ? w.prClosed(fix.pr_number) : m.reasons[fix.outcome_reason ?? 'agent_error'].split('. ')[0];
    case 'cancelled': return fix.outcome_reason === 'cancelled_by_person' ? '' : m.reasons[fix.outcome_reason ?? 'cancelled_by_person'].split('. ')[0];
  }
}

export interface FixGroupRows { items: Fix[]; load: 'loading' | 'error' | 'ready'; more: boolean; loadingMore: boolean; onMore: () => void }

interface FixesPageProps {
  groups: Record<FixGroup, FixGroupRows>;
  // The counts of the whole workspace (the rail's), shown when nothing is filtered.
  counts?: { needs_person: number; running: number };
  repos: Repo[];
  repoId?: string;
  connector?: FixConnector;
  onFilter: (repoId: string | undefined, connector: FixConnector | undefined) => void;
  timezone: string;
  now: number;
  onRetry: () => void;
  // Links into the app, given by the caller so this view needs no router.
  conversationLink: (id: string, label: string) => ReactNode;
  settingsLink: (label: string) => ReactNode;
  inboxLink: (label: string) => ReactNode;
}

const order: FixGroup[] = ['needs_person', 'running', 'done'];
const connectors: FixConnector[] = ['claude_issue', 'copilot_issue', 'cursor_api'];

function Rows({ items, timezone, now, conversationLink }: { items: Fix[]; timezone: string; now: number; conversationLink: FixesPageProps['conversationLink'] }) {
  const t = m.fixes;
  return <div className="af-tbl-wrap panel"><table className="af-table">
    <thead><tr>{(['conversation', 'state', 'repo', 'pr', 'age', 'agent', 'by'] as const).map((key) =>
      <th key={key} scope="col" className={key === 'age' ? 'af-num' : undefined}>{t.columns[key]}</th>)}</tr></thead>
    <tbody>{items.map((fix) => {
      const view = fixView(fix, now);
      const href = safeHref(fix.pr_url);
      return <tr key={fix.id} data-tone={view.tone}>
        <td className="af-c-subj"><div className="af-subj">{conversationLink(fix.conversation_id, fix.conversation_subject || t.unknownConversation)}
          <span>{fix.contact_name ? `${fix.contact_name} · ` : ''}{t.fixRef(shortId(fix.id))}</span></div></td>
        <td className="af-c-state"><StateLabel tone={view.tone}>{view.badge}</StateLabel><span className="af-why">{whyOf(fix, timezone, now)}</span></td>
        <td className="af-c-repo af-mono">{fix.repo_full_name}</td>
        <td className="af-c-pr">{fix.pr_number === null ? '—' : href ? <a href={href} target="_blank" rel="noopener noreferrer">#{fix.pr_number}</a> : `#${fix.pr_number}`}</td>
        <td className="af-num af-c-age">{ageOf(fix, now)}</td>
        <td className="af-c-agent"><span className="af-vchip">{m.vendor(fix.connector)}</span></td>
        <td className="af-c-by">{fix.created_by_kind === 'token' ? t.approvedBy(fix.created_by_name) : fix.created_by_name}</td>
      </tr>;
    })}</tbody>
  </table></div>;
}

// FixesPage lists every fix of the workspace in three groups: those that need a person, running, done.
export function FixesPage({ groups, counts, repos, repoId, connector, onFilter, timezone, now, onRetry, conversationLink, settingsLink, inboxLink }: FixesPageProps) {
  const t = m.fixes;
  const filtered = Boolean(repoId || connector);
  const all = order.map((group) => groups[group]);
  const failed = all.some((rows) => rows.load === 'error');
  const loading = all.some((rows) => rows.load === 'loading');
  const empty = !failed && !loading && all.every((rows) => rows.items.length === 0);
  const repoName = repos.find((repo) => repo.id === repoId)?.full_name;
  return (
    <main className="settings-page af-fixes" aria-busy={loading}>
      <header className="af-page-head"><div><h1>{t.heading}</h1><p>{t.description}</p></div>{settingsLink(m.settings)}</header>
      {(!empty || filtered) && <div className="af-bar-row">
        <label><span className="sr-only">{m.repository}</span><select value={repoId ?? ''} onChange={(event) => onFilter(event.target.value || undefined, connector)}>
          <option value="">{t.allRepos}</option>{repos.map((repo) => <option key={repo.id} value={repo.id}>{repo.full_name}</option>)}</select></label>
        <label><span className="sr-only">{m.agent}</span><select value={connector ?? ''} onChange={(event) => onFilter(repoId, (event.target.value || undefined) as FixConnector | undefined)}>
          <option value="">{t.allAgents}</option>{connectors.map((kind) => <option key={kind} value={kind}>{m.vendor(kind)}</option>)}</select></label>
      </div>}
      {failed && <div className="af-empty panel" role="alert"><p className="error-copy">{t.failed}</p><button type="button" onClick={onRetry}>{m.tryAgain}</button></div>}
      {empty && !filtered && <div className="af-empty panel"><b>{t.emptyTitle}</b><span>{t.emptyHelp}</span>{inboxLink(t.openInbox)}</div>}
      {empty && filtered && <div className="af-empty panel"><b>{t.filteredEmpty(connector && m.vendor(connector), repoName)}</b><span>{t.filteredHelp}</span>
        <button type="button" onClick={() => onFilter(undefined, undefined)}>{t.showAll}</button></div>}
      {!failed && !empty && order.map((group) => {
        const rows = groups[group];
        if (rows.load === 'ready' && rows.items.length === 0) return null;
        const count = !filtered && counts && group !== 'done' ? counts[group] : rows.load === 'ready' && !rows.more ? rows.items.length : undefined;
        return <section key={group} className="af-grp" aria-labelledby={`af-g-${group}`} aria-busy={rows.load === 'loading'}>
          <h2 id={`af-g-${group}`}>{t.groups[group]}{count !== undefined && <span className="af-count">{count}</span>}</h2>
          {t.groupHelp[group] && <p className="af-help">{t.groupHelp[group]}</p>}
          {rows.load === 'loading' ? <div role="status" className="af-skeletons panel af-tbl-wrap"><span className="af-skeleton" /><span className="af-skeleton" /><span className="sr-only">{t.loading}</span></div>
            : <Rows items={rows.items} timezone={timezone} now={now} conversationLink={conversationLink} />}
          {rows.more && <button type="button" className="af-more" disabled={rows.loadingMore} onClick={rows.onMore}>{t.more}</button>}
        </section>;
      })}
    </main>
  );
}
