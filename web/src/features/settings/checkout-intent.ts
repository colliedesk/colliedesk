const storageKey = 'colliedesk.checkout-intent';

// A checkout intent is the plan a visitor came to buy (the `buy` parameter of the billing link). It
// lives in sessionStorage so it survives the identity-provider sign-in and workspace sign-up that
// may sit between the link and the billing page; it dies with the tab.
export function rememberCheckoutIntent(plan: string): void {
  try {
    window.sessionStorage.setItem(storageKey, plan);
  } catch {
    // Storage can be blocked; the visitor then picks the plan on the billing page.
  }
}

export function readCheckoutIntent(): string | undefined {
  try {
    return window.sessionStorage.getItem(storageKey) || undefined;
  } catch {
    return undefined;
  }
}

export function clearCheckoutIntent(): void {
  try {
    window.sessionStorage.removeItem(storageKey);
  } catch {
    // Nothing was stored.
  }
}
