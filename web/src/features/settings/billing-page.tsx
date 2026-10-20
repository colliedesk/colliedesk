import type { Billing, CheckoutPlan } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatDay } from '../../lib/format-date';
import { messages } from '../../messages';
import { billingMessages } from './billing-messages';
import { PlanPicker } from './plan-picker';
import { SettingsNav } from './settings-nav';

interface BillingPageProps {
  billing: Billing;
  timezone: string;
  // checkoutReturned is set when the browser comes back from a successful checkout.
  checkoutReturned?: boolean;
  failed?: boolean;
  // busy is true while a checkout is starting or open; the plan buttons wait so a second click cannot open a second one.
  busy?: boolean;
  onCheckout: (plan: CheckoutPlan) => void;
  onManage: () => void;
}

// periodNote says when the paid plan renews, or when it ends if it was cancelled.
function periodNote(billing: Billing, timezone: string): string | null {
  if (billing.complimentary) return null;
  if (billing.plan === 'trial') return billingMessages.trialEndsOn(formatDay(billing.trial_ends_at, timezone));
  if (billing.plan === 'none') return billingMessages.trialEnded(formatDay(billing.trial_ends_at, timezone));
  if (!billing.current_period_end) return null;
  const when = formatDay(billing.current_period_end, timezone);
  return billing.cancel_at_period_end || billing.status === 'canceled' ? billingMessages.cancelsOn(when) : billingMessages.renewsOn(when);
}

const isPaid = (billing: Billing) => billing.plan !== 'trial' && billing.plan !== 'none';
// The server names the plan with more storage it sells; without one the cap is raised by asking us.
const paidStorageNext = (billing: Billing) =>
  billing.storage_upgrade_plan ? billingMessages.storageUpgradeTo(messages.plans[billing.storage_upgrade_plan]) : billingMessages.storageContactUs;
const paidStorageWarningNext = (billing: Billing) =>
  billing.storage_upgrade_plan ? billingMessages.storageWarningUpgradeTo(messages.plans[billing.storage_upgrade_plan]) : billingMessages.storageContactUs;

// StorageFull says what the cap does once it is reached, which differs by plan: on a paid plan customer
// email is always stored, on the trial and when locked it is refused. Below the cap the 80% warning
// of the page applies.
function StorageFull({ billing }: { billing: Billing }) {
  const limit = billing.limits.storage_bytes;
  if (limit === null || billing.usage.storage_bytes < limit) return null;
  const paid = isPaid(billing);
  const upgrade = paid ? paidStorageNext(billing) : billingMessages.storageUpgradeTrial;
  return <p role="alert" aria-label="Attachment storage" className="error-copy">
    {paid ? billingMessages.storageFullPaid(upgrade) : billingMessages.storageFullTrial(upgrade)}</p>;
}

// Usage shows its number and a bar; the bar turns amber from 80% so a nearly full limit stands out.
function Usage({ label, text, used, limit }: { label: string; text: string; used: number; limit: number }) {
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
  return (
    <div className="usage">
      <span>{text}</span>
      <div className="usage-bar" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={limit} aria-valuenow={used}
        data-level={percent >= 80 ? 'high' : 'ok'}><span style={{ width: `${percent}%` }} /></div>
    </div>
  );
}

export function BillingPage({ billing, timezone, checkoutReturned = false, failed = false, busy = false, onCheckout, onManage }: BillingPageProps) {
  const note = periodNote(billing, timezone);
  const storageLimit = billing.limits.storage_bytes;
  const warnAt = billing.limits.storage_warn_bytes;
  const storageNearlyFull = warnAt !== null && billing.usage.storage_bytes >= warnAt && !(storageLimit !== null && billing.usage.storage_bytes >= storageLimit);
  const manage = billing.status !== 'none' && !billing.complimentary;
  return (
    <main className="settings-page panel billing-page">
      <header><h1>{billingMessages.billingHeading}</h1><p>{billingMessages.billingDescription}</p></header>
      <div className="settings-layout">
        {/* Only admins reach this page, and an admin sees every settings section. */}
        <SettingsNav page="/settings/billing" shown={{ general: true, team: true, email: true, webForm: true, savedReplies: true, exportData: true, billing: true }} />
        <div className="settings-sections">
          {failed && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}
          {checkoutReturned && <p role="status">{billingMessages.checkoutReturned}</p>}
          {billing.active_subscriptions > 1 && <p role="alert" className="error-copy">{billingMessages.duplicateSubscriptions}</p>}
          {billing.status === 'past_due' && <p role="alert" className="error-copy">{billingMessages.pastDueNotice}</p>}
          {isPaid(billing) && billing.usage.agents > billing.limits.agents && <p role="alert" aria-label={billingMessages.usageAgents} className="error-copy">
            {messages.agentsOverPlan(billing.usage.agents - billing.limits.agents, messages.plans[billing.plan])}</p>}
          <section className="current-plan-card" aria-labelledby="current-plan-heading">
            <div className="current-plan">
              <h2 className="settings-group-label" id="current-plan-heading">{billingMessages.currentPlan}</h2>
              <p className="current-plan-name">{billing.complimentary
                ? billingMessages.complimentaryPlan(messages.plans[billing.plan], billing.complimentary_until ? formatDay(billing.complimentary_until, timezone) : null)
                : messages.plans[billing.plan]}</p>
              {note && <p className="muted-copy">{note}</p>}
              {manage && <div className="button-row"><button type="button" onClick={onManage}>{messages.manageBilling}</button></div>}
              {manage && <small className="muted-copy">{billingMessages.manageBillingHelp}</small>}
            </div>
            <div className="usage-list">
              {storageNearlyFull && storageLimit !== null && <p role="status" className="warning-note">
                {(() => {
                  const percent = Math.floor((billing.usage.storage_bytes / storageLimit) * 100);
                  const used = formatBytes(billing.usage.storage_bytes);
                  return isPaid(billing)
                    ? billingMessages.storageWarningPaid(percent, used, formatBytes(storageLimit), paidStorageWarningNext(billing))
                    : messages.storageWarning(percent, used, formatBytes(storageLimit));
                })()}</p>}
              <StorageFull billing={billing} />
              <Usage label={billingMessages.usageAgents} text={billingMessages.agentsUsage(billing.usage.agents, billing.limits.agents, billing.usage.pending_invites)}
                used={billing.usage.agents + billing.usage.pending_invites} limit={billing.limits.agents} />
              {storageLimit !== null && <Usage label={billingMessages.usageStorage} text={billingMessages.storageUsage(formatBytes(billing.usage.storage_bytes, 'down'), formatBytes(storageLimit))}
                used={billing.usage.storage_bytes} limit={storageLimit} />}
              {billing.limits.replies_per_24h !== null && <Usage label={billingMessages.usageReplies} text={billingMessages.repliesUsage(billing.usage.replies_last_24h, billing.limits.replies_per_24h)}
                used={billing.usage.replies_last_24h} limit={billing.limits.replies_per_24h} />}
            </div>
          </section>
          {(billing.plan === 'trial' || billing.plan === 'none') && <section className="settings-card" aria-labelledby="upgrade-heading">
            <div className="section-heading"><h2 id="upgrade-heading">{billingMessages.upgradeHeading}</h2><p>{billingMessages.upgradeHelp}</p></div>
            <div className="settings-body"><PlanPicker sellable={billing.checkout_plans} refusals={billing.checkout_refusals} busy={busy} onChoose={onCheckout} /></div>
          </section>}
        </div>
      </div>
    </main>
  );
}
