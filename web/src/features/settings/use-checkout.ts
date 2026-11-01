import { useMutation } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import type { CheckoutPlan } from '../../api/schemas';
import { openCheckout } from '../billing/open-checkout';
import { rememberPendingCheckout } from './checkout-pending';

// useCheckout starts a plan checkout and opens the billing portal. The plan is kept while the provider's page
// is open, so the page the checkout returns to can name the order. A refusal the server explained comes back
// as its words; any other failure is true, which the page words itself and answers with Try again.
export function useCheckout() {
  const checkout = useMutation({
    mutationFn: (plan: CheckoutPlan) => { rememberPendingCheckout(plan); return api.startCheckout(plan); },
    onSuccess: (link) => openCheckout(link.url),
  });
  const portal = useMutation({ mutationFn: () => api.openBillingPortal(), onSuccess: (link) => window.location.assign(link.url) });
  const { mutate, variables } = checkout;
  return {
    start: mutate,
    retry: () => { if (variables) mutate(variables); },
    // starting is the plan whose checkout is opening.
    starting: checkout.isPending ? variables : undefined,
    failed: checkout.error instanceof InvalidRequestError ? checkout.error.message : checkout.isError,
    openPortal: portal.mutate,
    portalFailed: portal.isError,
  };
}
