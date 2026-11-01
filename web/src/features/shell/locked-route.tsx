import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { ExportAllData } from '../settings/export-all-data';
import { readPlanIntent } from '../settings/plan-intent';
import { useCheckout } from '../settings/use-checkout';
import { LockedScreen } from './locked-screen';

// LockedRoute is the whole view of a locked workspace. Only admins read billing and may pay or export.
export function LockedRoute({ me }: { me: Me }) {
  const client = useQueryClient();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin });
  const founding = useQuery({ queryKey: ['founding'], queryFn: api.foundingSlots, enabled: isAdmin, staleTime: 60_000 });
  const placesLeft = founding.data?.open && founding.data.taken !== null ? Math.max(0, founding.data.slots - founding.data.taken) : undefined;
  const { start, retry, starting, failed, openPortal, portalFailed } = useCheckout();
  // The session is gone once the call succeeds; refetching me switches the shell to the signed-out state.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  return <LockedScreen isAdmin={isAdmin} billing={billing.data} adminNames={me.plan?.admins} placesLeft={placesLeft} preferredPlan={readPlanIntent()}
    checkoutFailed={failed || portalFailed} starting={starting} signOutFailed={signOut.isError}
    exportAction={<ExportAllData />} onCheckout={start} onRetryCheckout={retry} onManage={() => openPortal()} onSignOut={() => signOut.mutate()} />;
}
