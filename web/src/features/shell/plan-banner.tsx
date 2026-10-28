import type { Billing, Me } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';

interface PlanBannerProps {
  plan: Me['plan'];
  isAdmin: boolean;
  // billing is read by admins only; without it the storage warning cannot be shown.
  billing?: Billing;
  now?: Date;
  // onChoosePlan is left out where the plan choice is already on screen.
  onChoosePlan?: () => void;
}

const dayMs = 24 * 60 * 60 * 1000;
// Browser clocks drift: a few minutes of slack keeps a brand new trial at "14 days", not "15".
const clockSlackMs = 5 * 60 * 1000;

// PlanBanner tells everyone in a trial workspace how long it lasts, and tells admins when the
// attachment storage of the trial is nearly full. Paid and self-hosted workspaces show nothing.
export function PlanBanner({ plan, isAdmin, billing, now, onChoosePlan }: PlanBannerProps) {
  // The day count follows the calendar day of the browser, so a tab left open overnight updates.
  const clock = useNow('UTC');
  if (plan && plan.plan !== 'trial' && plan.agents_over > 0 && isAdmin) {
    return <section className="plan-banner" aria-label={messages.planNotices}>
      <p role="alert" className="plan-banner-warning">{messages.agentsOverPlan(plan.agents_over, messages.plans[plan.plan])}</p>
    </section>;
  }
  if (plan?.plan !== 'trial') return null;
  const msLeft = Date.parse(plan.trial_ends_at) - (now ?? clock).getTime();
  const daysLeft = Math.max(1, Math.ceil((msLeft - clockSlackMs) / dayMs));
  const limit = billing?.limits.storage_bytes ?? null;
  const warnAt = billing?.limits.storage_warn_bytes ?? null;
  const used = billing?.usage.storage_bytes ?? 0;
  const storageFull = isAdmin && limit !== null && warnAt !== null && used >= warnAt;
  return (
    <section className="plan-banner" aria-label={messages.planNotices}>
      <p>
        <strong>{messages.trialDaysLeft(daysLeft)}</strong>
        {plan.trial_extended && <span> {messages.trialExtendedNote}</span>}
      </p>
      {storageFull && <p role="status" className="plan-banner-warning">
        {messages.storageWarning(Math.floor((used / limit) * 100), formatBytes(used), formatBytes(limit))}
      </p>}
      {isAdmin && onChoosePlan && <button type="button" onClick={onChoosePlan}>{messages.choosePlan}</button>}
    </section>
  );
}
