import { useState } from 'react';
import type { ReactNode } from 'react';
import { formatShortDate } from '../../lib/format-date';
import type { FixConnector, FixDetail, FixEvent, Repo } from './autofix-api';
import { ExternalIcon, ReleaseIcon, RetryIcon, WrenchIcon } from './autofix-icons';
import { autofixMessages as m } from './autofix-messages';
import { fixView, minutesIn, reachedVendor, safeHref, shortId, waitsForAssignment, waitsOnGitHub } from './fix-view';
import type { FixAction, Stop, Tone } from './fix-view';

// What a new fix of this conversation would use: the repository and agent it starts with.
export interface SendTarget { repo?: Repo; connector?: FixConnector }

export interface AutofixBlockProps {
  load: 'loading' | 'error' | 'ready';
  // The conversation's latest fix, with its timeline; null when none was sent.
  fix: FixDetail | null;
  target: SendTarget;
  customerName?: string;
  timezone: string;
  now: number;
  busy?: boolean;
  // What went wrong with the last action, or what the return from GitHub said.
  problem?: string;
  notice?: string;
  onSend: () => void;
  onAction: (action: FixAction) => void;
  onRetryLoad: () => void;
  // A link into Settings → Autofix, given by the caller so this view needs no router.
  settingsLink: (label: string, primary?: boolean) => ReactNode;
}

const toneClass: Record<Tone, string> = { off: 'af-st-off', running: 'af-st-open', attention: 'af-st-warn', done: 'af-st-ok', stopped: 'af-st-err' };

export function StateLabel({ tone, children }: { tone: Tone; children: ReactNode }) {
  return <span className={`status ${toneClass[tone]}`}>{children}</span>;
}

export function Track({ stops, skipped }: { stops: Stop[]; skipped: boolean }) {
  return <ol className="af-track" aria-label={skipped ? m.trackSkipped : m.trackLabel}>
    {stops.map((stop, index) => <li key={index} className={`af-${stop.mark}`} aria-current={stop.mark === 'now' || stop.mark === 'hold' || stop.mark === 'halt' || stop.mark === 'cut' ? 'step' : undefined}>
      <span className="af-bar" aria-hidden="true" /><span className="af-lbl">{stop.label}</span></li>)}
  </ol>;
}

// External links open in a new tab, and only when they are https (fix-view.ts).
export function OutLink({ href, children, className }: { href: string | null | undefined; children: ReactNode; className?: string }) {
  const safe = safeHref(href);
  return safe ? <a href={safe} target="_blank" rel="noopener noreferrer" className={className}>{children}</a> : <>{children}</>;
}

const summaryOf = (event: FixEvent | undefined) => (typeof event?.data.summary === 'string' && event.data.summary ? event.data.summary : undefined);

// eventLine says one entry of the timeline in words; the source chip names GitHub or Cursor when they reported it. login is
// the GitHub account that created the fix's issue (the event's actor is whoever recorded it, not a GitHub login).
function eventLine(event: FixEvent, login: string | null): { text: ReactNode; error: boolean } {
  const e = m.event;
  const name = event.actor_name;
  const said = summaryOf(event);
  const error = event.to_state === 'failed';
  switch (event.kind) {
    case 'created': return { text: <><b>{name ?? m.someone}</b> {e.created(event.actor_kind === 'token')}</>, error };
    case 'cancel': return { text: <b>{e.cancel(name)}</b>, error };
    case 'link_pr': return { text: e.link_pr(name, event.data.pr_number), error };
    case 'unlink_pr': return { text: e.unlink_pr(name), error };
    case 'mark_deployed': return { text: e.mark_deployed(name), error };
    case 'unmark_deployed': return { text: e.unmark_deployed(name), error };
    default:
      if (event.kind === 'issue_created' || event.data.issue_number !== undefined && event.to_state === 'sent') {
        return { text: e.issue_created(event.data.issue_number, event.data.github_login ?? login), error };
      }
      if (said) return { text: name ? `${name}: ${said}` : said, error };
      return { text: event.to_state ? e.toState[event.to_state] : event.kind.replace(/_/g, ' '), error };
  }
}

function Activity({ events, login, timezone, now }: { events: FixEvent[]; login: string | null; timezone: string; now: number }) {
  const [all, setAll] = useState(false);
  const newest = [...events].sort((a, b) => b.created_at.localeCompare(a.created_at) || b.id - a.id);
  const shown = all ? newest : newest.slice(0, 5);
  return <>
    <div className="af-evts-head"><span>{m.activity}</span>
      {newest.length > 5 && <button type="button" className="link-button" aria-expanded={all} onClick={() => setAll(!all)}>{all ? m.fewerEvents : m.allEvents(newest.length)}</button>}</div>
    <ol className="af-evts" aria-label={m.activityLabel}>{shown.map((event) => {
      const line = eventLine(event, login);
      const source = m.sources[event.actor_kind];
      return <li key={event.id}><time dateTime={event.created_at}>{formatShortDate(event.created_at, timezone, new Date(now))}</time>
        <span className={line.error ? 'af-err' : undefined}>{line.text}{source && <span className="af-src">{source}</span>}</span></li>;
    })}</ol>
  </>;
}

function Facts({ rows }: { rows: [string, ReactNode][] }) {
  const shown = rows.filter(([, value]) => value !== null && value !== undefined && value !== false && value !== '');
  return shown.length > 0 ? <dl className="af-kv">{shown.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl> : null;
}

const Box = ({ tone = '', role, children }: { tone?: '' | 'danger' | 'note' | 'ok'; role?: 'status' | 'alert'; children: ReactNode }) =>
  <div className={`af-oc${tone ? ` af-${tone}` : ''}`} role={role}>{children}</div>;

// The sentence under the track: what is happening now, or why it stopped, and what to do.
function Outcome({ fix, now, at, customerName }: { fix: FixDetail; now: number; at: (value: string) => string; customerName?: string }) {
  const last = [...fix.events].reverse().find((event) => event.to_state === fix.state);
  switch (fix.state) {
    case 'sending':
      if (waitsOnGitHub(fix, now)) return <Box tone="note" role="status"><span><b>{m.stuckSending(minutesIn(fix, now))}</b> {m.stuckSendingHelp}</span></Box>;
      return <Box role="status"><span className="af-row"><span className="af-spinner" aria-hidden="true" />
        {fix.connector === 'cursor_api' ? <span><b>{m.sendingCursor}</b> {m.sendingCursorHelp}</span>
          : <span><b>{m.sendingIssue(fix.github_login)}</b> {m.sendingIssueHelp}</span>}</span></Box>;
    case 'sent':
      if (waitsForAssignment(fix)) return <Box tone="note" role="status"><span><b>{m.assignOnGitHub(fix.issue_number)}</b> {m.assignOnGitHubHelp}</span></Box>;
      if (fix.stale) {
        return <Box tone="note" role="status"><span><b>{m.stale(fix.connector, fix.issue_number, minutesIn(fix, now))}</b> {m.staleHelp}</span>
          <ul>{m.staleChecklist[fix.connector].map((item) => <li key={item}>{item}</li>)}</ul></Box>;
      }
      return <p>{m.sent(fix.issue_number, fix.connector)}</p>;
    case 'working': return <p>{m.working(fix.connector, fix.issue_number)}</p>;
    case 'pr_open': return <p>{m.prOpen}</p>;
    case 'merged': return <p>{m.merged(customerName)}</p>;
    case 'deployed': return <Box tone="ok"><span><b>{m.releasedLabel}</b> · {m.released(fix.deployed_by_name, at(fix.deployed_at ?? fix.state_changed_at))}</span></Box>;
    case 'failed': {
      const reason = fix.outcome_reason ?? 'agent_error';
      const said = summaryOf(last);
      const text = reason === 'agent_error' ? <><b>{m.failedTitle.agent_error(fix.connector)}</b>{said ? m.vendorSaid(said) : '.'}</>
        : reason === 'vendor_rejected' ? <><b>{m.reasons.vendor_rejected}</b>{said ? m.vendorSaid(said) : ''}</> : m.reasons[reason];
      return <Box tone="danger"><span>{text}</span></Box>;
    }
    case 'cancelled': {
      const reason = fix.outcome_reason ?? 'cancelled_by_person';
      // A disconnected repository cancels a fix the agent may have started, like a person's cancel.
      const disconnected = reason === 'repo_disconnected' && reachedVendor(fix);
      if (reason !== 'cancelled_by_person' && !disconnected) return <Box><span>{m.reasons[reason]}</span></Box>;
      const cancel = [...fix.events].reverse().find((event) => event.kind === 'cancel');
      // Cursor did not stop the run when asked: it may still open a pull request.
      const stuck = fix.events.find((event) => event.kind === 'cursor_cancel_failed');
      const issueLeft = disconnected ? (fix.issue_url ? <>{m.disconnectedIssue} <OutLink href={fix.issue_url}>{m.stopOnGitHub}</OutLink>.</> : null)
        : <>{m.cancelledIssue} <OutLink href={fix.issue_url}>{m.stopOnGitHub}</OutLink>.</>;
      return <Box><span><b>{disconnected ? m.reasons.repo_disconnected : m.cancelledBy(cancel?.actor_name ?? null, at(fix.state_changed_at))}</b>{' '}
        {fix.connector === 'cursor_api' ? (stuck ? <>{m.cursorNotStopped} <OutLink href={typeof stuck.data.vendor_url === 'string' ? stuck.data.vendor_url : fix.vendor_url}>{m.stopOnCursor}</OutLink>.</> : disconnected ? m.disconnectedCursor : m.cancelledCursor) : issueLeft}
        {fix.pr_number !== null && <> <b>{m.cancelledPR(fix.pr_number)}</b></>}</span></Box>;
    }
  }
}

export function AutofixBlockView({ load, fix, target, customerName, timezone, now, busy = false, problem, notice, onSend, onAction, onRetryLoad, settingsLink }: AutofixBlockProps) {
  const at = (value: string) => formatShortDate(value, timezone, new Date(now));
  const heading = (status?: ReactNode, sub?: string) => <header>
    <h3><WrenchIcon />{m.heading}</h3>{sub && <span className="af-att">{sub}</span>}{status}</header>;
  const notices = <>{notice && <p role="status" className="af-notice">{notice}</p>}{problem && <p role="alert" className="error-copy">{problem}</p>}</>;
  if (load !== 'ready') {
    return <section className="af-block panel" aria-label={m.heading} aria-busy={load === 'loading'}>
      {heading()}
      {load === 'loading'
        ? <div role="status" className="af-skeletons"><span className="af-skeleton" /><span className="af-skeleton" /><span className="af-skeleton" /><span className="sr-only">{m.loading}</span></div>
        : <Box tone="danger" role="alert"><span><b>{m.loadFailed}</b></span><div className="af-acts"><button type="button" onClick={onRetryLoad}>{m.tryAgain}</button></div></Box>}
    </section>;
  }
  if (!fix) {
    const { repo, connector } = target;
    return <section className="af-block panel" aria-label={m.heading}>
      {heading(<StateLabel tone="off">{m.badge.none}</StateLabel>)}
      {notices}
      {!repo ? <><p>{m.noRepo}</p><div className="af-acts">{settingsLink(m.openSettings)}</div></>
        : !connector ? <><p>{m.noAgent}</p><div className="af-acts">{settingsLink(m.openSettings)}</div></>
          : <>
            <p>{m.intro}</p>
            <Facts rows={[[m.repository, <span className="af-mono">{repo.full_name}</span>],
              [m.agent, <>{m.vendor(connector)}{repo.default_connector === connector && <span className="af-muted"> · {m.workspaceDefault}</span>}</>]]} />
            <div className="af-acts"><button type="button" className="primary-action" disabled={busy} onClick={onSend}><WrenchIcon />{m.sendToAgent}</button></div>
          </>}
    </section>;
  }
  const view = fixView(fix, now, fix.events);
  const actions = new Set(view.actions);
  const button = (action: FixAction, label: ReactNode, kind: '' | 'primary' | 'danger' | 'ghost' = '') =>
    <button key={action} type="button" className={kind === 'ghost' ? 'af-ghost' : kind ? `${kind}-action${kind === 'danger' ? ' af-push' : ''}` : undefined} disabled={busy} onClick={() => onAction(action)}>{label}</button>;
  const primary = (action: FixAction) => (view.primary === action ? 'primary' : '');
  const issue = fix.issue_number !== null && <OutLink href={fix.issue_url}>#{fix.issue_number} {fix.sent_title}<ExternalIcon /></OutLink>;
  const pr = fix.pr_number !== null && <OutLink href={fix.pr_url}>#{fix.pr_number}<ExternalIcon /></OutLink>;
  // The page to open on GitHub or at Cursor for this state.
  const prShown = fix.pr_url !== null && (fix.state === 'pr_open' || fix.state === 'merged' || fix.state === 'deployed' || fix.state === 'cancelled');
  const open = fix.state === 'sending' || (fix.state === 'failed' && (fix.outcome_reason === 'pr_closed' || !reachedVendor(fix))) ? null
    : prShown ? <OutLink key="open" href={fix.pr_url} className="af-btn"><ExternalIcon />{m.openPR}</OutLink>
      : fix.issue_url ? <OutLink key="open" href={fix.issue_url} className="af-btn"><ExternalIcon />{fix.stale && fix.issue_number !== null ? m.openIssueN(fix.issue_number) : m.openIssue}</OutLink>
        : fix.connector === 'cursor_api' && fix.vendor_url ? <OutLink key="open" href={fix.vendor_url} className="af-btn"><ExternalIcon />{m.openSession}</OutLink> : null;
  const next = fix.attempt_no + 1;
  const buttons: ReactNode[] = [];
  for (const action of view.actions) {
    switch (action) {
      case 'assign_on_github': buttons.push(<OutLink key={action} href={fix.issue_url} className="af-btn primary-action"><ExternalIcon />{m.assignCopilot}</OutLink>); break;
      case 'mark_released': buttons.push(button(action, <><ReleaseIcon />{m.markReleased}</>, 'primary')); break;
      case 'draft_reply': buttons.push(button(action, m.draftReply, 'primary')); break;
      case 'unmark_released': buttons.push(button(action, m.unmarkReleased, 'ghost')); break;
      case 'retry': buttons.push(button(action, <><RetryIcon />{m.retry(next)}</>, primary(action))); break;
      case 'send_again': buttons.push(button(action, <><RetryIcon />{m.sendAgain}</>, 'primary')); break;
      case 'link_pr': buttons.push(button(action, fix.outcome_reason === 'pr_closed' ? m.linkAnotherPR : m.linkPR)); break;
      case 'unlink_pr': buttons.push(button(action, m.unlinkPR)); break;
      case 'replace_key': buttons.push(<span key={action}>{settingsLink(m.replaceKey)}</span>); break;
      case 'settings': buttons.push(<span key={action}>{settingsLink(m.settings)}</span>); break;
      case 'cancel': break;
    }
  }
  // Open sits after the primary action and before the others; Cancel always last, pushed right.
  const leading = view.primary ? buttons.slice(0, 1) : [];
  const rest = view.primary ? buttons.slice(1) : buttons;
  const cursor = fix.connector === 'cursor_api';
  const help = fix.state === 'merged' ? m.mergeHelp : fix.state === 'deployed' ? m.releasedHelp
    : actions.has('retry') ? m.retryHelp(fix.attempt_no) : undefined;
  return (
    <section className="af-block panel" aria-label={m.heading} data-tone={view.tone}>
      {heading(<StateLabel tone={view.tone}>{view.badge}</StateLabel>, m.attempt(shortId(fix.id), fix.attempt_no))}
      <p className="af-handoff"><b>{m.vendor(fix.connector)}</b><span className="af-arr" aria-hidden="true">·</span><span>{m.handoff(fix.connector)}</span><span className="af-mono">{fix.repo_full_name}</span></p>
      <Track stops={view.track} skipped={view.skipped} />
      <Outcome fix={fix} now={now} at={at} customerName={customerName} />
      {notices}
      <Facts rows={[
        [m.issue, !cursor && issue && <>{issue}{fix.github_login && <span className="af-muted"> · {m.createdBy(fix.github_login)}</span>}</>],
        [m.cursorSession, cursor && (fix.vendor_url ? <OutLink href={fix.vendor_url}>{m.session} bc-{shortId(fix.id)}…<ExternalIcon /></OutLink> : <span className="af-muted">{m.noSession}</span>)],
        [m.pullRequest, pr && <>{pr} <span className="af-muted">· {fix.pr_linked_by === 'person' ? m.linkedByPerson : m.foundByColliedesk}</span></>],
        [m.mergedLabel, fix.merge_sha && <><span className="af-mono">{fix.merge_sha.slice(0, 7)}</span>{fix.merged_by_login && <span className="af-muted"> · {fix.merged_by_login}{fix.merged_at ? `, ${at(fix.merged_at)}` : ''}</span>}</>],
        [m.sentBy, <>{fix.created_by_name} <span className="af-muted">· {fix.created_by_kind === 'token' ? m.approvedRequest : at(fix.created_at)}</span></>],
      ]} />
      {fix.state !== 'sending' && fix.events.length > 0 && <Activity events={fix.events} login={fix.github_login} timezone={timezone} now={now} />}
      <div className="af-acts">{leading}{open}{rest}{actions.has('cancel') && button('cancel', m.cancel, 'danger')}</div>
      {help && <p className="af-help">{help}</p>}
    </section>
  );
}
