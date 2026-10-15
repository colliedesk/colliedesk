import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { PlanBanner } from './plan-banner';

// PlanNotices feeds the trial banner. Only a trial admin reads billing (for the storage warning);
// everyone else gets the days left from the plan that /auth/me already carries.
export function PlanNotices({ me }: { me: Me }) {
  const navigate = useNavigate();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && me.plan?.plan === 'trial' });
  return <PlanBanner plan={me.plan} isAdmin={isAdmin} billing={billing.data}
    onChoosePlan={() => void navigate({ to: '/settings/billing', search: { checkout: '' } })} />;
}
