import type { Billing } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { messages } from '../../messages';
import { TriangleAlertIcon } from '../../ui/icons';
import { billingMessages } from './billing-messages';

export const isPaid = (billing: Billing) => billing.plan !== 'trial' && billing.plan !== 'none';
// The server names the plan with more storage it sells; without one the cap is raised by asking us.
const paidStorageNext = (billing: Billing) =>
  billing.storage_upgrade_plan ? billingMessages.storageUpgradeTo(messages.plans[billing.storage_upgrade_plan]) : billingMessages.storageContactUs;
const paidStorageWarningNext = (billing: Billing) =>
  billing.storage_upgrade_plan ? billingMessages.storageWarningUpgradeTo(messages.plans[billing.storage_upgrade_plan]) : billingMessages.storageContactUs;

// StorageNotice says what the storage cap does as it nears and once it is reached, which differs by plan: on a
// paid plan customer email is always stored, on the trial and when locked it is refused. Below 80% nothing shows.
function StorageNotice({ billing }: { billing: Billing }) {
  const limit = billing.limits.storage_bytes;
  const warnAt = billing.limits.storage_warn_bytes;
  const used = billing.usage.storage_bytes;
  const paid = isPaid(billing);
  if (limit !== null && used >= limit) {
    const upgrade = paid ? paidStorageNext(billing) : billingMessages.storageUpgradeTrial;
    return <p role="alert" aria-label="Attachment storage" className="inline-alert"><TriangleAlertIcon tone="danger" /><span>
      {paid ? billingMessages.storageFullPaid(upgrade) : billingMessages.storageFullTrial(upgrade)}</span></p>;
  }
  if (limit === null || warnAt === null || used < warnAt) return null;
  const percent = Math.floor((used / limit) * 100);
  const text = paid
    ? billingMessages.storageWarningPaid(percent, formatBytes(used), formatBytes(limit), paidStorageWarningNext(billing))
    : messages.storageWarning(percent, formatBytes(used), formatBytes(limit));
  return <p role="status" className="inline-alert"><TriangleAlertIcon /><span>{text}</span></p>;
}

// BillingAlerts are the notices above Plan and billing: billing switched off, a failed payment, a double
// subscription, agents beyond the plan and the storage cap.
export function BillingAlerts({ billing, switchedOff, portalFailed }: { billing: Billing; switchedOff: boolean; portalFailed: boolean }) {
  return (
    <>
      {switchedOff && <p role="note" className="inline-alert"><TriangleAlertIcon /><span>{billingMessages.billingSwitchedOff}</span></p>}
      {portalFailed && <p role="alert" className="inline-alert"><TriangleAlertIcon tone="danger" /><span>{messages.billingActionFailed}</span></p>}
      {billing.active_subscriptions > 1 && <p role="alert" className="inline-alert"><TriangleAlertIcon tone="danger" /><span>{billingMessages.duplicateSubscriptions}</span></p>}
      {billing.status === 'past_due' && <p role="alert" className="inline-alert"><TriangleAlertIcon tone="danger" /><span>{billingMessages.pastDueNotice}</span></p>}
      {isPaid(billing) && billing.usage.agents > billing.limits.agents && <p role="alert" aria-label={billingMessages.usageAgents} className="inline-alert">
        <TriangleAlertIcon tone="danger" /><span>{messages.agentsOverPlan(billing.usage.agents - billing.limits.agents, messages.plans[billing.plan])}</span></p>}
      <StorageNotice billing={billing} />
    </>
  );
}
