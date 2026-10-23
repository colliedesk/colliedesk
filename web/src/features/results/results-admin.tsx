import { useEffect, useRef, useState } from 'react';
import type { ShareLink } from '../../api/results';
import { formatDate, formatDay } from '../../lib/format-date';
import { resultsMessages as m } from './results-messages';

export interface ResultsAdminProps {
  timezone: string;
  link?: ShareLink | null;
  linkFailed: boolean;
  // created is the address of a link made a moment ago: shown once.
  created?: string;
  replacedOld: boolean;
  busy: boolean;
  shareFailed: boolean;
  onCreate: () => void;
  onTurnOff: () => void;
  weekly?: { subscribed: boolean; can_send: boolean; next_at: string; timezone: string } | null;
  weeklyBusy: boolean;
  weeklyFailed: boolean;
  onWeekly: (subscribed: boolean) => void;
}

// The admins' block of the results page: the share link and their own weekly email.
export function ResultsAdmin(props: ResultsAdminProps) {
  const { timezone, link, linkFailed, created, replacedOld, busy, shareFailed, onCreate, onTurnOff, weekly, weeklyBusy, weeklyFailed, onWeekly } = props;
  const [copied, setCopied] = useState(false);
  // The buttons are disabled while a change runs, which drops the focus; when it ends the focus goes to
  // Copy link (a new link) or to the create button (the link turned off), never back to the page top.
  const createButton = useRef<HTMLButtonElement>(null);
  const copyButton = useRef<HTMLButtonElement>(null);
  const focusAfter = useRef<'copy' | 'create'>(undefined);
  useEffect(() => {
    if (busy || !focusAfter.current) return;
    (focusAfter.current === 'copy' && copyButton.current ? copyButton.current : createButton.current)?.focus();
    focusAfter.current = undefined;
  }, [busy, created, link]);
  const copy = async () => {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created);
      setCopied(true);
    } catch {
      // The field stays selected for a manual copy.
    }
  };
  return <section className="results-admin" aria-labelledby="results-admin">
    <header><h2 id="results-admin">{m.admin.heading}</h2><p className="results-muted">{m.admin.note}</p></header>
    <div className="results-admin-grid">
      <section className="results-card results-admin-card" aria-labelledby="results-share">
        <h3 id="results-share">{m.admin.shareHeading}</h3>
        <p>{m.admin.shareAbout}</p>
        {linkFailed ? <p role="alert" className="error-copy">{m.admin.shareLoadFailed}</p> : <>
          <p className="results-share-state" data-on={link ? true : undefined}><span className="results-dot" aria-hidden="true" />
            {link ? m.admin.on(formatDay(link.created_at, timezone), link.created_by_name, link.last_viewed_at ? formatDate(link.last_viewed_at, timezone) : null) : m.admin.off}</p>
          {created && <div className="results-share-new">
            <p>{m.admin.copyNow}</p>
            <div className="results-share-copy">
              {/* The whole address wraps: nothing of the token is hidden behind a scroll. */}
              <p className="results-share-url" aria-label={m.admin.linkLabel}><code>{created}</code></p>
              <button type="button" ref={copyButton} onClick={() => void copy()}><span role="status">{copied ? m.admin.copied : m.admin.copy}</span></button>
            </div>
            {replacedOld && <p className="results-muted">{m.admin.oldTurnedOff}</p>}
          </div>}
          <div className="button-row">
            <button type="button" ref={createButton} disabled={busy} onClick={() => { setCopied(false); focusAfter.current = 'copy'; onCreate(); }}>{link ? m.admin.createNew : m.admin.create}</button>
            {link && <button type="button" className="danger-action" disabled={busy} onClick={() => { focusAfter.current = 'create'; onTurnOff(); }}>{m.admin.turnOff}</button>}
          </div>
          {shareFailed && <p role="alert" className="error-copy">{m.admin.shareFailed}</p>}
          <p className="results-muted">{link ? m.admin.onceNote : m.admin.offNote}</p>
        </>}
      </section>
      <section className="results-card results-admin-card" id="weekly-email" aria-labelledby="results-weekly">
        <h3 id="results-weekly">{m.admin.weeklyHeading}</h3>
        {weekly && <>
          <p>{m.admin.weeklyAbout(weekly.timezone)}</p>
          <label className="results-switch">
            <input type="checkbox" role="switch" checked={weekly.subscribed} disabled={(!weekly.can_send && !weekly.subscribed) || weeklyBusy} onChange={(event) => onWeekly(event.target.checked)} />
            <span>{m.admin.weeklyToggle}</span>
          </label>
          {weekly.can_send ? <p className="results-muted">{weekly.subscribed ? m.admin.weeklyOn(formatDay(weekly.next_at, weekly.timezone)) : m.admin.weeklyOff}</p>
            : <div className="results-notice results-notice-warn"><p>{m.admin.noMail}</p><a href={m.admin.noMailURL} target="_blank" rel="noreferrer">{m.admin.noMailLink}</a></div>}
          {weeklyFailed && <p role="alert" className="error-copy">{m.admin.weeklyFailed}</p>}
        </>}
      </section>
    </div>
  </section>;
}
