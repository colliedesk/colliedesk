import { useState } from 'react';
import type { Billing, Me } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatMonthDay } from '../../lib/format-date';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';
import { ClockIcon, TriangleAlertIcon, XIcon } from '../../ui/icons';
import type { PlanIntent } from '../settings/plan-intent';
import { closeTrialStrip, isTrialStripClosed, type TrialStripStage } from './trial-strip-closed';

interface PlanBannerProps {
  plan: Me['plan'];
  isAdmin: boolean;
  // workspaceId lets anyone close the trial strip; without it the strip cannot be closed.
  workspaceId?: string;
  // billing is read by admins only; without it the storage warning cannot be shown.
  billing?: Billing;
  // timezone is the agent's, so the end date is the day the trial ends where they work.
  timezone?: string;
  now?: Date;
  // planIntent is the plan the visitor chose on the website; the button then names it.
  planIntent?: PlanIntent;
  // onChoosePlan is left out where the plan choice is already on screen.
  onChoosePlan?: () => void;
}

const dayMs = 24 * 60 * 60 * 1000;
// Browser clocks drift: a few minutes of slack keeps a brand new trial at "14 days", not "15".
const clockSlackMs = 5 * 60 * 1000;
// From this many days on, the strip says what happens when the trial ends.
const lockWarningDays = 3;

// PlanBanner tells everyone in a trial workspace when it ends, and tells admins when the attachment storage
// of the trial is nearly full. Paid and self-hosted workspaces show nothing. Anyone can close the trial
// strip; it comes back once when the lock gets close. The storage warning cannot be closed.
export function PlanBanner({ plan, isAdmin, workspaceId, billing, timezone = 'UTC', now, planIntent, onChoosePlan }: PlanBannerProps) {
  // The day count follows the calendar day of the browser, so a tab left open overnight updates.
  const clock = useNow('UTC');
  const [closedNow, setClosedNow] = useState<TrialStripStage | null>(null);
  if (plan && plan.plan !== 'trial' && plan.agents_over > 0 && isAdmin) {
    return <section className="plan-banner" aria-label={messages.planNotices}>
      <p role="alert" className="plan-banner-warning"><TriangleAlertIcon />{messages.agentsOverPlan(plan.agents_over, messages.plans[plan.plan])}</p>
    </section>;
  }
  if (plan?.plan !== 'trial') return null;
  const msLeft = Date.parse(plan.trial_ends_at) - (now ?? clock).getTime();
  const daysLeft = Math.max(1, Math.ceil((msLeft - clockSlackMs) / dayMs));
  const ending = daysLeft <= lockWarningDays;
  const stage: TrialStripStage = ending ? 'ending' : 'early';
  const closed = workspaceId !== undefined && (closedNow === stage || isTrialStripClosed(workspaceId, stage));
  const close = () => {
    if (workspaceId === undefined) return;
    closeTrialStrip(workspaceId, stage);
    setClosedNow(stage);
  };
  const ends = messages.trialEnds(formatMonthDay(plan.trial_ends_at, timezone, now ?? clock));
  // The warning, the question for the admin and the note about an extension each follow the date as a sentence.
  const sentences = [ending && messages.trialLocks, !isAdmin && messages.trialAskAdmin, plan.trial_extended && messages.trialExtendedNote].filter(Boolean);
  const limit = billing?.limits.storage_bytes ?? null;
  const warnAt = billing?.limits.storage_warn_bytes ?? null;
  const used = billing?.usage.storage_bytes ?? 0;
  const storageFull = isAdmin && limit !== null && warnAt !== null && used >= warnAt;
  if (closed && !storageFull) return null;
  return (
    <section className="plan-banner" aria-label={messages.planNotices}>
      {!closed && <p>
        {ending ? <TriangleAlertIcon /> : <span className="icon-quiet"><ClockIcon /></span>}
        {sentences.length === 0 ? <strong>{ends}</strong> : <span><strong>{ends}.</strong> {sentences.join(' ')}</span>}
      </p>}
      {storageFull && <p role="status" className="plan-banner-warning"><TriangleAlertIcon />
        {messages.storageWarning(Math.floor((used / limit) * 100), formatBytes(used), formatBytes(limit))}</p>}
      {isAdmin && onChoosePlan && <button type="button" onClick={onChoosePlan}>{planIntent === 'team25' ? messages.buyPlan(messages.plans.team25) : messages.choosePlan}</button>}
      {!closed && workspaceId !== undefined && <button type="button" className="icon-button plan-banner-close" aria-label={messages.hideTrialNotice}
        data-tip={messages.hideTrialNotice} onClick={close}><XIcon /></button>}
    </section>
  );
}
