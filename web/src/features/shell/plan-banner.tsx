import type { Billing, Me } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { messages } from '../../messages';

interface PlanBannerProps {
  plan: Me['plan'];
  isAdmin: boolean;
  // billing is read by admins only; without it the storage warning cannot be shown.
  billing?: Billing;
  now?: Date;
  onChoosePlan: () => void;
}

const dayMs = 24 * 60 * 60 * 1000;
// A trial starts at 14 days (domain.TrialLength); more left than that means it was extended.
const standardTrialDays = 14;

// PlanBanner tells everyone in a trial workspace how long it lasts, and tells admins when the
// attachment storage of the trial is nearly full. Paid and self-hosted workspaces show nothing.
export function PlanBanner({ plan, isAdmin, billing, now = new Date(), onChoosePlan }: PlanBannerProps) {
  if (plan?.plan !== 'trial') return null;
  const msLeft = Date.parse(plan.trial_ends_at) - now.getTime();
  const daysLeft = Math.max(1, Math.ceil(msLeft / dayMs));
  const limit = billing?.limits.storage_bytes ?? null;
  const warnAt = billing?.limits.storage_warn_bytes ?? null;
  const used = billing?.usage.storage_bytes ?? 0;
  const storageFull = isAdmin && limit !== null && warnAt !== null && used >= warnAt;
  return (
    <section className="plan-banner" aria-label={messages.planNotices}>
      <p>
        <strong>{messages.trialDaysLeft(daysLeft)}</strong>
        {daysLeft > standardTrialDays && <span> {messages.trialExtendedNote}</span>}
      </p>
      {storageFull && <p role="status" className="plan-banner-warning">
        {messages.storageWarning(Math.floor((used / limit) * 100), formatBytes(used), formatBytes(limit))}
      </p>}
      {isAdmin && <button type="button" className="primary-action" onClick={onChoosePlan}>{messages.choosePlan}</button>}
    </section>
  );
}
