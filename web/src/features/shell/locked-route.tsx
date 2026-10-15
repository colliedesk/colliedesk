import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { CheckoutPlan, Me } from '../../api/schemas';
import { ExportAllData } from '../settings/export-all-data';
import { LockedScreen } from './locked-screen';

// LockedRoute is the whole view of a locked workspace. Only admins read billing and may pay or export.
export function LockedRoute({ me }: { me: Me }) {
  const client = useQueryClient();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin });
  const checkout = useMutation({ mutationFn: (plan: CheckoutPlan) => api.startCheckout(plan), onSuccess: (link) => window.location.assign(link.url) });
  // The session is gone once the call succeeds; refetching me switches the shell to the signed-out state.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  return <LockedScreen isAdmin={isAdmin} billing={billing.data} timezone={me.timezone}
    checkoutFailed={checkout.isError} signOutFailed={signOut.isError}
    exportAction={<ExportAllData />} onCheckout={(plan) => checkout.mutate(plan)} onSignOut={() => signOut.mutate()} />;
}
