import type { Billing, CheckoutPlan } from '../../api/schemas';
import { formatStorage } from '../../lib/format-storage';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface BillingPageProps {
  billing: Billing;
  timezone: string;
  // checkoutReturned is set when the browser comes back from a successful checkout.
  checkoutReturned?: boolean;
  failed?: boolean;
  onCheckout: (plan: CheckoutPlan) => void;
  onManage: () => void;
}

const upgrades: { plan: CheckoutPlan; label: string }[] = [
  { plan: 'team_monthly', label: messages.upgradeTeamMonthly },
  { plan: 'team_yearly', label: messages.upgradeTeamYearly },
  { plan: 'founding', label: messages.upgradeFounding },
];

// periodNote says when the paid plan renews, or when it ends if it was cancelled.
function periodNote(billing: Billing, timezone: string): string | null {
  if (billing.complimentary) return null;
  if (billing.plan === 'trial') return messages.trialEndsOn(formatDate(billing.trial_ends_at, timezone));
  if (billing.plan === 'none') return messages.trialEnded(formatDate(billing.trial_ends_at, timezone));
  if (!billing.current_period_end) return null;
  const when = formatDate(billing.current_period_end, timezone);
  return billing.cancel_at_period_end || billing.status === 'canceled' ? messages.cancelsOn(when) : messages.renewsOn(when);
}

// StorageNotice warns at 80% of the attachment storage cap and says what happens at the cap.
function StorageNotice({ billing }: { billing: Billing }) {
  const { storage_bytes: limit, storage_warn_bytes: warn } = billing.limits;
  const used = billing.usage.storage_bytes;
  if (limit === null || warn === null || used < warn) return null;
  const upgrade = billing.plan === 'founding' ? messages.storageUpgradeFounding : messages.storageUpgradeTrial;
  // On the Founding plan customer email is always stored; on the trial and when locked it is refused at the cap.
  const full = billing.plan === 'founding' ? messages.storageFullFounding(upgrade) : messages.storageFullTrial(upgrade);
  if (used >= limit) return <p role="alert" aria-label="Attachment storage" className="error-copy">{full}</p>;
  return <p role="status" aria-label="Attachment storage" className="muted-copy">{messages.storageWarning(Math.floor(used * 100 / limit), upgrade)}</p>;
}

export function BillingPage({ billing, timezone, checkoutReturned = false, failed = false, onCheckout, onManage }: BillingPageProps) {
  const note = periodNote(billing, timezone);
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
        <ul className="muted-copy">
          <li>{messages.agentsUsage(billing.usage.agents, billing.limits.agents, billing.usage.pending_invites)}</li>
          {billing.limits.storage_bytes !== null && <li>{messages.storageUsage(formatStorage(billing.usage.storage_bytes, 'down'), formatStorage(billing.limits.storage_bytes))}</li>}
        </ul>
        <StorageNotice billing={billing} />
      </section>
      {(billing.plan === 'trial' || billing.plan === 'none') && <section aria-labelledby="upgrade-heading">
        <div className="section-heading"><div><h2 id="upgrade-heading">{messages.upgradeHeading}</h2></div></div>
        <div className="button-row">{upgrades.map((item) =>
          <button key={item.plan} type="button" className="primary-action" onClick={() => onCheckout(item.plan)}>{item.label}</button>)}</div>
      </section>}
      {billing.status !== 'none' && !billing.complimentary && <section aria-labelledby="manage-billing-heading">
        <div className="section-heading"><div><h2 id="manage-billing-heading">{messages.manageBilling}</h2><p>{messages.manageBillingHelp}</p></div></div>
        <button type="button" onClick={onManage}>{messages.manageBilling}</button>
      </section>}
    </main>
  );
}
