import { localPath } from '../../lib/workspace-host';

// A plan intent is the plan a visitor came to look at (the `plan` parameter of the website's trial
// link). Unlike a checkout intent (`buy`) it opens no payment window: the trial starts as usual and the
// plan picker only puts that plan first and marks it. It lives in sessionStorage so it survives the
// identity-provider sign-in and workspace sign-up between the link and the billing page; it dies with
// the tab or after a day.
export type PlanIntent = 'team25';

const storageKey = 'colliedesk.plan-intent';
const maxAgeMs = 24 * 60 * 60_000;

// parsePlanIntent accepts team25 only: any other value in an address is ignored.
export function parsePlanIntent(value: unknown): PlanIntent | undefined {
  return value === 'team25' ? value : undefined;
}

export function rememberPlanIntent(plan: PlanIntent): void {
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify({ plan, at: Date.now() }));
  } catch {
    // Storage can be blocked; the visitor then finds the plan on the billing page.
  }
}

export function readPlanIntent(): PlanIntent | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (typeof stored !== 'object' || stored === null) return undefined;
    const { plan, at } = stored as { plan?: unknown; at?: unknown };
    return typeof at === 'number' && Date.now() - at <= maxAgeMs ? parsePlanIntent(plan) : undefined;
  } catch {
    // Storage is blocked or holds something this code did not write.
    return undefined;
  }
}

export function clearPlanIntent(): void {
  try {
    window.sessionStorage.removeItem(storageKey);
  } catch {
    // Nothing was stored.
  }
}

// signInPlanIntent is the plan a sign-in page serves. As for a checkout intent, the address decides:
// a plan there is remembered again (the identity provider returns to that same address); a plain
// cloud sign-in is an unrelated visit and forgets an earlier one; a one-time link keeps it.
export function signInPlanIntent({ plan, token }: { plan?: string; token?: string }): PlanIntent | undefined {
  const named = parsePlanIntent(plan);
  if (named) {
    rememberPlanIntent(named);
    return named;
  }
  if (token) return readPlanIntent();
  clearPlanIntent();
  return undefined;
}

// planIntentFromNext splits the page a hand-off opens on a workspace host into the plan it carries
// (`?plan=team25`, which only travels in that address: the host's storage is not the sign-in page's)
// and the page itself without it.
export function planIntentFromNext(next: string | undefined): { plan?: PlanIntent; path: string } {
  const path = localPath(next);
  const url = new URL(path, 'http://workspace.invalid');
  const plan = parsePlanIntent(url.searchParams.get('plan'));
  if (!plan) return { path };
  url.searchParams.delete('plan');
  return { plan, path: url.pathname + url.search + url.hash };
}
