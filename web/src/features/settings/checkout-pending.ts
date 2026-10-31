import { checkoutPlanSchema } from '../../api/schemas';
import type { CheckoutPlan } from '../../api/schemas';

const storageKey = 'colliedesk.checkout-pending';
const maxAgeMs = 24 * 60 * 60_000;

// The plan an admin chose is kept in sessionStorage while the checkout runs on the provider's page, so the
// page it returns to can name the order while the payment is confirmed.
export function rememberPendingCheckout(plan: CheckoutPlan): void {
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify({ plan, at: Date.now() }));
  } catch {
    // Storage can be blocked; the page then names no order.
  }
}

export function readPendingCheckout(): CheckoutPlan | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (typeof stored !== 'object' || stored === null) return undefined;
    const { plan, at } = stored as { plan?: unknown; at?: unknown };
    const parsed = checkoutPlanSchema.safeParse(plan);
    return parsed.success && typeof at === 'number' && Date.now() - at <= maxAgeMs ? parsed.data : undefined;
  } catch {
    // Storage is blocked or holds something this code did not write.
    return undefined;
  }
}

export function clearPendingCheckout(): void {
  try {
    window.sessionStorage.removeItem(storageKey);
  } catch {
    // Nothing was stored.
  }
}
