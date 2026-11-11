// Signup attribution is the post or ad a visitor came from: the utm_* keys of the sign-in link and the
// click IDs TikTok (ttclid) and Meta (fbclid) add to ad links. Only listed keys with plain values are kept,
// as colliedesk.com writes them, so nothing else in the address (a sign-in token above all) is stored.
// It lives in sessionStorage so it survives the identity-provider round trip before sign-up, and dies
// with the tab or after a day. The server checks every value again.
const keys = ['utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term', 'ttclid', 'fbclid'] as const;
const clickIDs = ['ttclid', 'fbclid'] as const;
const plainValue = /^[A-Za-z0-9._-]{1,300}$/;
const storageKey = 'colliedesk.signup-attribution';
const maxAgeMs = 24 * 60 * 60_000;

type Values = Partial<Record<(typeof keys)[number], string>>;
// SignupAttribution is the request's `attribution`: the values, and when a click ID was first seen.
export type SignupAttribution = Values & { click_id_seen_at?: string };

interface Stored {
  values: Values;
  at: number;
  clickSeenAt?: number;
}

export function attributionFromSearch(search: string): Values {
  const params = new URLSearchParams(search);
  const values: Values = {};
  for (const key of keys) {
    const value = params.get(key);
    if (value !== null && plainValue.test(value)) values[key] = value;
  }
  return values;
}

function sameClick(a: Values, b: Values): boolean {
  return clickIDs.some((key) => a[key] !== undefined && a[key] === b[key]);
}

function readStored(): Stored | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (typeof stored !== 'object' || stored === null) return undefined;
    const { values, at, clickSeenAt } = stored as { values?: unknown; at?: unknown; clickSeenAt?: unknown };
    if (typeof values !== 'object' || values === null || typeof at !== 'number' || Date.now() - at > maxAgeMs) return undefined;
    // Re-read through the same filter: storage may hold something this code did not write.
    const kept = attributionFromSearch(new URLSearchParams(values as Record<string, string>).toString());
    if (Object.keys(kept).length === 0) return undefined;
    return { values: kept, at, ...(typeof clickSeenAt === 'number' ? { clickSeenAt } : {}) };
  } catch {
    // Storage is blocked or holds something this code did not write.
    return undefined;
  }
}

// rememberSignupAttribution keeps the attribution of a sign-in address. An address without any keeps what
// an earlier landing in this tab remembered; one with some replaces it. A click ID already remembered keeps
// the time it was first seen, so the identity provider's return is not a second click.
export function rememberSignupAttribution(search: string): void {
  const values = attributionFromSearch(search);
  if (Object.keys(values).length === 0) return;
  const previous = readStored();
  const hasClick = clickIDs.some((key) => values[key] !== undefined);
  const clickSeenAt = hasClick ? (previous?.clickSeenAt !== undefined && sameClick(previous.values, values) ? previous.clickSeenAt : Date.now()) : undefined;
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify({ values, at: Date.now(), ...(clickSeenAt !== undefined ? { clickSeenAt } : {}) }));
  } catch {
    // Storage can be blocked; the trial then starts without attribution.
  }
}

export function readSignupAttribution(): SignupAttribution | undefined {
  const stored = readStored();
  if (!stored) return undefined;
  return { ...stored.values, ...(stored.clickSeenAt !== undefined ? { click_id_seen_at: new Date(stored.clickSeenAt).toISOString() } : {}) };
}

export function clearSignupAttribution(): void {
  try {
    window.sessionStorage.removeItem(storageKey);
  } catch {
    // Nothing was stored.
  }
}
