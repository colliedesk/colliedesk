import type { Billing, CheckoutPlan } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { planOptions } from './plan-options';

interface BillingPageProps {
  billing: Billing;
  timezone: string;
  // checkoutReturned is set when the browser comes back from a successful checkout.
  checkoutReturned?: boolean;
  failed?: boolean;
  onCheckout: (plan: CheckoutPlan) => void;
  onManage: () => void;
}

// periodNote says when the paid plan renews, or when it ends if it was cancelled.
function periodNote(billing: Billing, timezone: string): string | null {
  if (billing.complimentary) return null;
  if (billing.plan === 'trial') return messages.trialEndsOn(formatDate(billing.trial_ends_at, timezone));
  if (billing.plan === 'none') return messages.trialEnded(formatDate(billing.trial_ends_at, timezone));
  if (!billing.current_period_end) return null;
  const when = formatDate(billing.current_period_end, timezone);
  return billing.cancel_at_period_end || billing.status === 'canceled' ? messages.cancelsOn(when) : messages.renewsOn(when);
}

// StorageFull says what the cap does once it is reached, which differs by plan: on Founding customer
// email is always stored, on the trial and when locked it is refused. Below the cap the 80% warning
// of the page applies.
function StorageFull({ billing }: { billing: Billing }) {
  const limit = billing.limits.storage_bytes;
  if (limit === null || billing.usage.storage_bytes < limit) return null;
  const founding = billing.plan === 'founding';
  const upgrade = founding ? messages.storageUpgradeFounding : messages.storageUpgradeTrial;
  return <p role="alert" aria-label="Attachment storage" className="error-copy">
    {founding ? messages.storageFullFounding(upgrade) : messages.storageFullTrial(upgrade)}</p>;
}

export function BillingPage({ billing, timezone, checkoutReturned = false, failed = false, onCheckout, onManage }: BillingPageProps) {
  const note = periodNote(billing, timezone);
  const storageLimit = billing.limits.storage_bytes;
  const warnAt = billing.limits.storage_warn_bytes;
  const storageNearlyFull = warnAt !== null && billing.usage.storage_bytes >= warnAt && !(storageLimit !== null && billing.usage.storage_bytes >= storageLimit);
  return (
    <main className="settings-page">
      <header><h1>{messages.billingHeading}</h1><p>{messages.billingDescription}</p></header>
      {failed && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}
      {checkoutReturned && <p role="status">{messages.checkoutReturned}</p>}
      {billing.active_subscriptions > 1 && <p role="alert" className="error-copy">{messages.duplicateSubscriptions}</p>}
      {billing.status === 'past_due' && <p role="alert" className="error-copy">{messages.pastDueNotice}</p>}
      <section aria-labelledby="current-plan-heading">
        <div className="section-heading"><div><h2 id="current-plan-heading">{messages.currentPlan}</h2></div></div>
        <p><strong>{billing.complimentary
          ? messages.complimentaryPlan(messages.plans[billing.plan], billing.complimentary_until ? formatDate(billing.complimentary_until, timezone) : null)
          : messages.plans[billing.plan]}</strong></p>
        {note && <p className="muted-copy">{note}</p>}
        {storageNearlyFull && storageLimit !== null && <p role="status" className="error-copy">
          {messages.storageWarning(Math.floor((billing.usage.storage_bytes / storageLimit) * 100), formatBytes(billing.usage.storage_bytes), formatBytes(storageLimit))}</p>}
        <ul className="muted-copy">
          <li>{messages.agentsUsage(billing.usage.agents, billing.limits.agents, billing.usage.pending_invites)}</li>
          {storageLimit !== null && <li>{messages.storageUsage(formatBytes(billing.usage.storage_bytes, 'down'), formatBytes(storageLimit))}</li>}
          {billing.limits.replies_per_24h !== null && <li>{messages.repliesUsage(billing.usage.replies_last_24h, billing.limits.replies_per_24h)}</li>}
        </ul>
        <StorageFull billing={billing} />
      </section>
      {(billing.plan === 'trial' || billing.plan === 'none') && <section aria-labelledby="upgrade-heading">
        <div className="section-heading"><div><h2 id="upgrade-heading">{messages.upgradeHeading}</h2></div></div>
        <div className="button-row">{planOptions.map((item) =>
          <button key={item.plan} type="button" className="primary-action" onClick={() => onCheckout(item.plan)}>{item.label}</button>)}</div>
      </section>}
      {billing.status !== 'none' && !billing.complimentary && <section aria-labelledby="manage-billing-heading">
        <div className="section-heading"><div><h2 id="manage-billing-heading">{messages.manageBilling}</h2><p>{messages.manageBillingHelp}</p></div></div>
        <button type="button" onClick={onManage}>{messages.manageBilling}</button>
      </section>}
    </main>
  );
}
