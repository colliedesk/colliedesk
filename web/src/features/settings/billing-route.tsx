import { useMutation, useQuery } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import type { CheckoutPlan } from '../../api/schemas';
import { api } from '../../api/client';
import { messages } from '../../messages';
import { BillingPage } from './billing-page';
import { clearCheckoutIntent, readCheckoutIntent } from './checkout-intent';

interface BillingRouteProps {
  // checkoutReturned is true on the page the checkout sends the browser back to.
  checkoutReturned?: boolean;
}

// While the payment provider confirms a new subscription the plan is still free; poll briefly.
const confirmPollMs = 2000;

export function BillingRoute({ checkoutReturned = false }: BillingRouteProps) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const enabled = config.data?.billing.enabled === true;
  const billing = useQuery({
    queryKey: ['billing'], queryFn: api.billing, enabled: isAdmin && enabled,
    refetchInterval: (query) => (checkoutReturned && (query.state.data?.plan === 'trial' || query.state.data?.plan === 'none') ? confirmPollMs : false),
  });
  const leave = { onSuccess: (link: { url: string }) => window.location.assign(link.url) };
  const checkout = useMutation({ mutationFn: (plan: CheckoutPlan) => api.startCheckout(plan), ...leave });
  const portal = useMutation({ mutationFn: () => api.openBillingPortal(), ...leave });
  // A remembered plan is acted on once, as soon as it is known whether this admin can buy it; every
  // other outcome drops it, so it can never fire later or loop.
  const intentHandled = useRef(false);
  const unpaid = billing.data?.plan === 'trial' || billing.data?.plan === 'none';
  const cannotBuy = (config.data && !enabled) || (me.data && !isAdmin);
  const { mutate: startCheckout } = checkout;
  useEffect(() => {
    if (intentHandled.current || !(cannotBuy || billing.data)) return;
    const wanted = readCheckoutIntent();
    if (!wanted) return;
    intentHandled.current = true;
    clearCheckoutIntent();
    const offered = billing.data?.checkout_plans.find((plan) => plan === wanted);
    if (offered && unpaid && !checkoutReturned) startCheckout(offered);
  }, [billing.data, cannotBuy, checkoutReturned, startCheckout, unpaid]);
  if (config.data && !enabled) return <main className="settings-page"><p className="muted-copy">{messages.billingNotEnabled}</p></main>;
  if (me.data && !isAdmin) return <main className="settings-page"><p className="muted-copy">{messages.billingAdminsOnly}</p></main>;
  if (!billing.data) return <main className="settings-page">{billing.isError && <p role="alert" className="error-copy">{messages.billingActionFailed}</p>}</main>;
  return <BillingPage billing={billing.data} timezone={me.data?.timezone ?? 'UTC'} checkoutReturned={checkoutReturned}
    failed={checkout.isError || portal.isError}
    onCheckout={(plan) => checkout.mutate(plan)} onManage={() => portal.mutate()} />;
}
