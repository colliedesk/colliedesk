import type { Billing, CheckoutPlan } from '../../api/schemas';
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
  if (billing.plan === 'trial') return messages.trialEndsOn(formatDate(billing.trial_ends_at, timezone));
  if (billing.plan === 'none') return messages.trialEnded(formatDate(billing.trial_ends_at, timezone));
  if (!billing.current_period_end) return null;
  const when = formatDate(billing.current_period_end, timezone);
  return billing.cancel_at_period_end || billing.status === 'canceled' ? messages.cancelsOn(when) : messages.renewsOn(when);
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
        <p><strong>{messages.plans[billing.plan]}</strong></p>
        {note && <p className="muted-copy">{note}</p>}
        <ul className="muted-copy">
          <li>{messages.agentsUsage(billing.usage.agents, billing.limits.agents, billing.usage.pending_invites)}</li>
        </ul>
      </section>
      {(billing.plan === 'trial' || billing.plan === 'none') && <section aria-labelledby="upgrade-heading">
        <div className="section-heading"><div><h2 id="upgrade-heading">{messages.upgradeHeading}</h2></div></div>
        <div className="button-row">{upgrades.map((item) =>
          <button key={item.plan} type="button" className="primary-action" onClick={() => onCheckout(item.plan)}>{item.label}</button>)}</div>
      </section>}
      {billing.status !== 'none' && <section aria-labelledby="manage-billing-heading">
        <div className="section-heading"><div><h2 id="manage-billing-heading">{messages.manageBilling}</h2><p>{messages.manageBillingHelp}</p></div></div>
        <button type="button" onClick={onManage}>{messages.manageBilling}</button>
      </section>}
    </main>
  );
}
