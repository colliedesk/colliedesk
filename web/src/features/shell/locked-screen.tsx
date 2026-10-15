import type { ReactNode } from 'react';
import type { Billing, CheckoutPlan } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { planOptions } from '../settings/plan-options';

interface LockedScreenProps {
  isAdmin: boolean;
  // billing is read by admins only. It tells a lapsed subscription from an ended trial.
  billing?: Billing;
  timezone: string;
  checkoutFailed?: boolean;
  signOutFailed?: boolean;
  exportAction?: ReactNode;
  onCheckout: (plan: CheckoutPlan) => void;
  onSignOut: () => void;
}

function heading(billing?: Billing) {
  if (!billing) return messages.lockedTitleGeneric;
  return billing.status === 'none' ? messages.lockedTitleTrial : messages.lockedTitleSubscription;
}

// LockedScreen replaces the whole workspace once the trial or subscription ended. It names what
// happened, what an admin can do (pay, take the data out) and what anyone can do (sign out).
export function LockedScreen({ isAdmin, billing, timezone, checkoutFailed = false, signOutFailed = false, exportAction, onCheckout, onSignOut }: LockedScreenProps) {
  const endedAt = billing && (billing.status === 'none' ? billing.trial_ends_at : billing.current_period_end ?? billing.trial_ends_at);
  return (
    <main className="settings-page panel locked-screen">
      <header>
        <h1>{heading(billing)}</h1>
        <p>{endedAt && `${messages.lockedSince(formatDate(endedAt, timezone))} `}{isAdmin ? messages.lockedAdminExplain : messages.lockedAgentExplain}</p>
      </header>
      {checkoutFailed && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}
      {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
      <section className="settings-card" aria-labelledby="locked-plan-heading">
        <div className="section-heading"><h2 id="locked-plan-heading">{messages.choosePlan}</h2></div>
        <div className="settings-body">
          {isAdmin
            ? <div className="button-row">{planOptions.map((option) =>
                <button key={option.plan} type="button" className="primary-action" onClick={() => onCheckout(option.plan)}>{option.label}</button>)}</div>
            : <p>{messages.askWorkspaceAdmin}</p>}
        </div>
      </section>
      {isAdmin && exportAction && <section className="settings-card" aria-labelledby="locked-export-heading">
        <div className="section-heading"><h2 id="locked-export-heading">{messages.exportHeading}</h2><p>{messages.exportDescription}</p></div>
        <div className="settings-body">{exportAction}</div>
      </section>}
      <section className="settings-card" aria-labelledby="locked-leave-heading">
        <div className="section-heading"><h2 id="locked-leave-heading">{messages.signOut}</h2><p className="muted-copy">{messages.lockedRetention}</p></div>
        <div className="settings-body"><div className="button-row"><button type="button" onClick={onSignOut}>{messages.signOut}</button></div></div>
      </section>
    </main>
  );
}
