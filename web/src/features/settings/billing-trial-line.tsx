import { Fragment } from 'react';
import type { Billing } from '../../api/schemas';
import { formatDay } from '../../lib/format-date';
import './purchase.css';
import { billingMessages } from './billing-messages';
import { gibOf } from './usage-format';

// TrialLine opens the plan choice with the trial in one line: when it ends, the seats and the storage in use.
export function TrialLine({ billing, timezone }: { billing: Billing; timezone: string }) {
  const when = formatDay(billing.trial_ends_at, timezone);
  const parts = [
    billing.plan === 'none' ? billingMessages.trialEndedLine(when) : billingMessages.trialEndsLine(when),
    billingMessages.agentsLine(billing.usage.agents + billing.usage.pending_invites, billing.limits.agents),
    billing.limits.storage_bytes === null ? undefined : billingMessages.storageLine(gibOf(billing.usage.storage_bytes), gibOf(billing.limits.storage_bytes)),
  ].filter(Boolean);
  return (
    <p className="billing-trial-line"><span><strong>{billingMessages.trialLabel}</strong></span>
      {parts.map((part) => <Fragment key={part}><span className="sep">·</span><span>{part}</span></Fragment>)}</p>
  );
}
