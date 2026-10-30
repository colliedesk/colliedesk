import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import type { CheckoutPlan, Me } from '../../api/schemas';
import { openCheckout } from '../billing/open-checkout';
import { ExportAllData } from '../settings/export-all-data';
import { readPlanIntent } from '../settings/plan-intent';
import { LockedScreen } from './locked-screen';

// LockedRoute is the whole view of a locked workspace. Only admins read billing and may pay or export.
export function LockedRoute({ me }: { me: Me }) {
  const client = useQueryClient();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin });
  const checkout = useMutation({ mutationFn: (plan: CheckoutPlan) => api.startCheckout(plan), onSuccess: (link) => openCheckout(link.url) });
  const portal = useMutation({ mutationFn: () => api.openBillingPortal(), onSuccess: (link) => window.location.assign(link.url) });
  // The session is gone once the call succeeds; refetching me switches the shell to the signed-out state.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  return <LockedScreen isAdmin={isAdmin} billing={billing.data} timezone={me.timezone} preferredPlan={readPlanIntent()}
    checkoutFailed={checkout.error instanceof InvalidRequestError ? checkout.error.message : checkout.isError || portal.isError} checkoutBusy={checkout.isPending} signOutFailed={signOut.isError}
    exportAction={<ExportAllData />} onCheckout={(plan) => checkout.mutate(plan)} onManage={() => portal.mutate()} onSignOut={() => signOut.mutate()} />;
}
