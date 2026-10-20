import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { CheckoutPlan, Me } from '../../api/schemas';
import { openCheckout } from '../billing/open-checkout';
import { ExportAllData } from '../settings/export-all-data';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';

// The locked screen and its plan cards load in their own chunk; most sessions never see them.
const lockedScreenPart = lazyPart(() => import('./locked-screen'));

// LockedRoute is the whole view of a locked workspace. Only admins read billing and may pay or export.
export function LockedRoute({ me }: { me: Me }) {
  const screen = lockedScreenPart.use();
  const client = useQueryClient();
  const isAdmin = me.agent.role === 'admin';
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin });
  const checkout = useMutation({ mutationFn: (plan: CheckoutPlan) => api.startCheckout(plan), onSuccess: (link) => openCheckout(link.url) });
  const portal = useMutation({ mutationFn: () => api.openBillingPortal(), onSuccess: (link) => window.location.assign(link.url) });
  // The session is gone once the call succeeds; refetching me switches the shell to the signed-out state.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  if (screen.status === 'error') return <main className="settings-page"><PartUnavailable /></main>;
  if (screen.status === 'loading') return <main className="settings-page" />;
  const { LockedScreen } = screen.module;
  return <LockedScreen isAdmin={isAdmin} billing={billing.data} timezone={me.timezone}
    checkoutFailed={checkout.isError || portal.isError} checkoutBusy={checkout.isPending} signOutFailed={signOut.isError}
    exportAction={<ExportAllData />} onCheckout={(plan) => checkout.mutate(plan)} onManage={() => portal.mutate()} onSignOut={() => signOut.mutate()} />;
}
