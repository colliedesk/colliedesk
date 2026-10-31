import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import type { Billing, CheckoutPlan } from '../../api/schemas';
import { messages } from '../../messages';
import { billingMessages } from '../settings/billing-messages';
import type { PlanIntent } from '../settings/plan-intent';
import { PlanPicker } from '../settings/plan-picker';
import '../settings/purchase.css';

interface LockedScreenProps {
  isAdmin: boolean;
  // billing is read by admins only. It tells a lapsed subscription from an ended trial.
  billing?: Billing;
  // adminNames are the workspace's admins, for a person who is not one.
  adminNames?: readonly string[];
  // placesLeft is how many Founding places are free, when the server has counted them.
  placesLeft?: number;
  // true for a generic failure (answered with Try again), or the server's words when it said why.
  checkoutFailed?: boolean | string;
  // starting is the plan whose checkout is opening.
  starting?: CheckoutPlan;
  signOutFailed?: boolean;
  // preferredPlan is the plan the visitor came for on the website; its card comes first.
  preferredPlan?: PlanIntent;
  exportAction?: ReactNode;
  onCheckout: (plan: CheckoutPlan) => void;
  onRetryCheckout?: () => void;
  // onManage opens the billing portal; offered when a subscription exists, so a failed card is fixed in place.
  onManage?: () => void;
  onSignOut: () => void;
}

// Only an admin reads billing, so only an admin is told which of the two ended; anyone else, and an admin
// whose billing is still loading, reads the plain fact.
function heading(billing?: Billing) {
  if (!billing) return billingMessages.lockedTitleGeneric;
  return billing.status === 'none' ? billingMessages.lockedTitleTrial : billingMessages.lockedTitleSubscription;
}

// LockedScreen replaces the whole workspace once the trial or subscription ended. It says the data is kept
// and gives an admin three ways on: choose a plan, export everything first, or ask for more time. Anyone
// else is told which admins to ask.
export function LockedScreen({ isAdmin, billing, adminNames = [], placesLeft, checkoutFailed = false, starting, signOutFailed = false, preferredPlan, exportAction, onCheckout, onRetryCheckout, onManage, onSignOut }: LockedScreenProps) {
  // A 402 can swap the inbox for this page; focus would stay on a removed node, so the heading takes it.
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => title.current?.focus(), []);
  const hasSubscription = billing !== undefined && billing.status !== 'none' && !billing.complimentary;
  const error = checkoutFailed === true ? billingMessages.checkoutFailed : checkoutFailed || undefined;
  return (
    <main className="settings-page panel locked-screen">
      <header>
        <h1 ref={title} tabIndex={-1}>{heading(billing)}</h1>
        <p>{isAdmin ? billingMessages.lockedAdminExplain : billingMessages.lockedAgentExplain(adminNames)}</p>
      </header>
      {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
      <div className="purchase-stack">
        {isAdmin ? <>
          <PlanPicker compact sellable={billing?.checkout_plans ?? []} refusals={billing?.checkout_refusals} seats={(billing?.usage.agents ?? 0) + (billing?.usage.pending_invites ?? 0)}
            placesLeft={placesLeft} starting={starting} preferredPlan={preferredPlan} error={error} onRetry={checkoutFailed === true ? onRetryCheckout : undefined} onChoose={onCheckout} />
          {hasSubscription && onManage && <div className="button-row"><button type="button" onClick={onManage}>{messages.manageBilling}</button></div>}
          <section className="locked-paths" aria-label={billingMessages.lockedPaths}>
            {exportAction && <div className="locked-export">{exportAction}<p>{messages.exportDescription}</p></div>}
            <p>{billingMessages.needMoreTime} <a className="touch-link" href={`mailto:${billingMessages.supportEmail}`}>{billingMessages.supportEmail}</a></p>
            <p>{billingMessages.lockedRetention}</p>
          </section>
        </> : <div className="button-row"><button type="button" onClick={onSignOut}>{messages.signOut}</button></div>}
      </div>
    </main>
  );
}
