import { useState } from 'react';
import type { Billing, CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';
import './purchase.css';
import { BillingAlerts, isPaid } from './billing-alerts';
import { billingMessages } from './billing-messages';
import { TrialLine } from './billing-trial-line';
import { MigrationDialog } from './migration-dialog';
import type { MigrationPlan } from './migration-dialog';
import { PaidBillingRows } from './paid-billing-rows';
import { PaymentConfirming } from './payment-confirming';
import type { PlanIntent } from './plan-intent';
import { PlanPicker } from './plan-picker';
import { purchasedPrice } from './plan-price-line';
import { SettingsNav } from './settings-nav';
import { formatDay } from '../../lib/format-date';

interface BillingPageProps {
  billing: Billing;
  timezone: string;
  // importDone is true once the workspace has a finished Freshdesk import, false when it has none, and
  // undefined while that is not known yet: the migration row waits for it.
  importDone?: boolean;
  // freshdeskAgents is the agent count of the Freshdesk account the workspace read, when it read one.
  freshdeskAgents?: number;
  // placesLeft is how many Founding places are free, when the server has counted them.
  placesLeft?: number;
  // checkoutReturned is set when the browser comes back from a successful checkout: until the payment
  // provider confirms the plan the page says so, and once it does a Founding or Team 25 buyer is welcomed.
  checkoutReturned?: boolean;
  // pendingPlan is the plan this browser sent to the checkout, which names the order while it is confirmed.
  pendingPlan?: CheckoutPlan;
  // welcome is true until the welcome after paying was dismissed once; it only opens right after a checkout.
  welcome?: boolean;
  onWelcomeDone?: () => void;
  // checkoutFailed is true for a generic failure (with Try again), or the server's words when it said why.
  checkoutFailed?: boolean | string;
  portalFailed?: boolean;
  // starting is the plan whose checkout is opening.
  starting?: CheckoutPlan;
  // preferredPlan is the plan the visitor chose on the website; its card leads.
  preferredPlan?: PlanIntent;
  onCheckout: (plan: CheckoutPlan) => void;
  onRetryCheckout?: () => void;
  onCheckAgain?: () => void;
  onManage: () => void;
  onRequestMigration: (input: { freshdesk_address: string; times: string }) => Promise<{ freshdesk_address: string; times: string; requested_at: string }>;
  // switchedOff: the operator switched billing off; the plan and the portal stay, nothing is offered.
  switchedOff?: boolean;
}

const isMigrationPlan = (plan: Billing['plan']): plan is MigrationPlan => plan === 'founding' || plan === 'team25';

export function BillingPage({ billing, timezone, importDone, freshdeskAgents, placesLeft, checkoutReturned = false, pendingPlan, welcome = false, onWelcomeDone, checkoutFailed = false,
  portalFailed = false, starting, preferredPlan, onCheckout, onRetryCheckout, onCheckAgain, onManage, onRequestMigration, switchedOff = false }: BillingPageProps) {
  const [requesting, setRequesting] = useState(false);
  const [welcomed, setWelcomed] = useState(false);
  const paid = isPaid(billing);
  const migrationPlan = isMigrationPlan(billing.plan) && !billing.complimentary ? billing.plan : undefined;
  const price = billing.purchased_plan ? purchasedPrice(billing.purchased_plan) : undefined;
  const periodEnd = billing.current_period_end ? formatDay(billing.current_period_end, timezone) : undefined;
  const welcoming = checkoutReturned && paid && welcome && !welcomed && migrationPlan !== undefined && periodEnd !== undefined;
  const dialogOpen = welcoming || (requesting && migrationPlan !== undefined);
  const confirming = checkoutReturned && !paid;
  const dismiss = () => { setRequesting(false); setWelcomed(true); onWelcomeDone?.(); };
  const checkoutError = checkoutFailed === true ? billingMessages.checkoutFailed : checkoutFailed || undefined;
  return (
    <main className="settings-page panel billing-page">
      <header><h1>{billingMessages.billingHeading}</h1><p>{billingMessages.billingDescription}</p></header>
      <div className="settings-layout">
        {/* Only admins reach this page, and an admin sees every settings section. */}
        <SettingsNav page="/settings/billing" shown={{ general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: true, importData: true, ai: true }} />
        <div className="settings-sections purchase-stack">
          <BillingAlerts billing={billing} switchedOff={switchedOff} portalFailed={portalFailed} />
          {checkoutReturned && paid && !welcoming && <p role="status">{billingMessages.checkoutConfirmed(messages.plans[billing.plan])}</p>}
          {paid
            ? <PaidBillingRows billing={billing} timezone={timezone} dialogOpen={dialogOpen} onManage={onManage} onRequestMigration={() => setRequesting(true)}
              migration={migrationPlan && importDone !== undefined ? { requestedAt: billing.migration_requested_at, done: importDone } : undefined} />
            : <>
              <TrialLine billing={billing} timezone={timezone} />
              {confirming && <PaymentConfirming plan={pendingPlan} onCheckAgain={onCheckAgain ?? (() => undefined)} />}
              {!confirming && !switchedOff && <PlanPicker sellable={billing.checkout_plans} refusals={billing.checkout_refusals} seats={billing.usage.agents + billing.usage.pending_invites}
                freshdeskAgents={freshdeskAgents} placesLeft={placesLeft} starting={starting} preferredPlan={preferredPlan} error={checkoutError}
                onRetry={checkoutFailed === true ? onRetryCheckout : undefined} onChoose={onCheckout} />}
            </>}
        </div>
      </div>
      {dialogOpen && migrationPlan && <MigrationDialog plan={migrationPlan} onSend={onRequestMigration} onDismiss={dismiss}
        welcome={welcoming && periodEnd ? { plan: migrationPlan, summary: price?.name ? `${price.name} · ${price.detail}` : messages.plans[migrationPlan], nextCharge: billingMessages.nextCharge(periodEnd, price?.amount) } : undefined} />}
    </main>
  );
}
