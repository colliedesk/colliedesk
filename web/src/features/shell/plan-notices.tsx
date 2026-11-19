import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { readPlanIntent } from '../settings/plan-intent';
import { can } from '../../lib/can';
import { lazyPart, Part } from '../../ui/lazy-part';
import { PlanBanner } from './plan-banner';

// The question for trial admins loads as its own chunk, and only where it can be asked.
const trialSurveyPart = lazyPart(() => import('./trial-survey'));

// PlanNotices feeds the trial banner. Only a trial admin reads billing (for the storage warning);
// everyone else gets the end date from the plan that /auth/me already carries.
// The billing page opens with the trial's end date and its plan choice, so the trial strip is left out there.
interface PlanNoticesProps {
  me: Me;
  onBillingPage?: boolean;
  analytics?: { enabled: boolean; realm: string | null };
}

export function PlanNotices({ me, onBillingPage = false, analytics }: PlanNoticesProps) {
  const navigate = useNavigate();
  const isAdmin = can(me, 'billing:manage');
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && me.plan?.plan === 'trial' });
  if (onBillingPage && me.plan?.plan === 'trial') return null;
  const asks = analytics?.realm === 'cloud' && isAdmin && me.plan?.plan === 'trial' && !me.plan.internal;
  return <PlanBanner plan={me.plan} isAdmin={isAdmin} workspaceId={me.workspace.id} billing={billing.data} timezone={me.timezone} planIntent={readPlanIntent()}
    onChoosePlan={() => void navigate({ to: '/settings/billing', search: { checkout: '' } })}
    aside={asks && <Part part={trialSurveyPart} render={({ TrialSurvey }) => <TrialSurvey me={me} analytics={analytics} />} />} />;
}
