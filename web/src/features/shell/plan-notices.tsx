import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { readPlanIntent } from '../settings/plan-intent';
import { PlanBanner } from './plan-banner';

// PlanNotices feeds the trial banner. Only a trial admin reads billing (for the storage warning);
// everyone else gets the end date from the plan that /auth/me already carries.
// The billing page opens with the trial's end date and its plan choice, so the trial strip is left out there.
export function PlanNotices({ me, onBillingPage = false }: { me: Me; onBillingPage?: boolean }) {
  const navigate = useNavigate();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && me.plan?.plan === 'trial' });
  if (onBillingPage && me.plan?.plan === 'trial') return null;
  return <PlanBanner plan={me.plan} isAdmin={isAdmin} billing={billing.data} timezone={me.timezone} planIntent={readPlanIntent()}
    onChoosePlan={() => void navigate({ to: '/settings/billing', search: { checkout: '' } })} />;
}
