const storageKey = 'colliedesk.checkout-intent';

// A checkout intent is the plan a visitor came to buy (the `buy` parameter of the billing link). It
// lives in sessionStorage so it survives the identity-provider sign-in and workspace sign-up that
// may sit between the link and the billing page; it dies with the tab or after 30 minutes, so an
// abandoned sign-in cannot start a checkout much later.
const maxAgeMs = 30 * 60_000;
export function rememberCheckoutIntent(plan: string): void {
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify({ plan, at: Date.now() }));
  } catch {
    // Storage can be blocked; the visitor then picks the plan on the billing page.
  }
}

export function readCheckoutIntent(): string | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (typeof stored !== 'object' || stored === null) return undefined;
    const { plan, at } = stored as { plan?: unknown; at?: unknown };
    return typeof plan === 'string' && plan !== '' && typeof at === 'number' && Date.now() - at <= maxAgeMs ? plan : undefined;
  } catch {
    // Storage is blocked or holds something this code did not write.
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

// signInCheckoutIntent is the plan a sign-in page serves. The plan rides in the sign-in address
// (`buy`) and the identity provider returns to that same address, so the address decides: a plan
// there is remembered again; a plain cloud sign-in is an unrelated visit and forgets an earlier
// one, or a later "Log in" in the same tab would still say "Join Founding". A one-time link keeps
// it: on a self-hosted server that link is how a buyer who was signed out gets back to the checkout.
export function signInCheckoutIntent({ buy, token }: { buy?: string; token?: string }): string | undefined {
  if (buy) {
    rememberCheckoutIntent(buy);
    return buy;
  }
  if (token) return readCheckoutIntent();
  clearCheckoutIntent();
  return undefined;
}
