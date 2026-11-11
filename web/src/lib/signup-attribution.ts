// Signup attribution is the post or ad a visitor came from: the utm_* keys of the sign-in link and the
// click IDs TikTok (ttclid) and Meta (fbclid) add to ad links. Only listed keys with plain values are kept,
// as colliedesk.com writes them, so nothing else in the address (a sign-in token above all) is stored.
// It lives in sessionStorage so it survives the identity-provider round trip before sign-up, and dies
// with the tab or after a day. The server checks every value again.
// The site whose link first opened the sign-in page in the tab is kept beside it, as its host name only: an
// AI assistant's link has no utm_* keys, so the referrer is all that says where the person came from.
const keys = ['utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term', 'ttclid', 'fbclid'] as const;
const clickIDs = ['ttclid', 'fbclid'] as const;
const plainValue = /^[A-Za-z0-9._-]{1,300}$/;
const storageKey = 'colliedesk.signup-attribution';
const referrerKey = 'colliedesk.signup-referrer';
const hostName = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
const maxAgeMs = 24 * 60 * 60_000;

type Values = Partial<Record<(typeof keys)[number], string>>;
// SignupAttribution is the request's `attribution`: the values, and when a click ID was first seen.
export type SignupAttribution = Values & { click_id_seen_at?: string; referrer?: string };

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

// referrerHost is the host name of an outside page that linked here, or '' for none, this app itself, or
// anything that is not a web address with a host name (an app link such as android-app://).
function referrerHost(referrer: string, ownHost: string): string {
  try {
    const url = new URL(referrer);
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return '';
    const host = url.hostname.toLowerCase();
    return host !== ownHost.toLowerCase() && host.length <= 253 && hostName.test(host) ? host : '';
  } catch {
    return '';
  }
}

function readReferrer(): string | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(referrerKey) ?? 'null');
    if (typeof stored !== 'object' || stored === null) return undefined;
    const { host, at } = stored as { host?: unknown; at?: unknown };
    if (typeof host !== 'string' || typeof at !== 'number' || Date.now() - at > maxAgeMs) return undefined;
    return host === '' || hostName.test(host) ? host : undefined;
  } catch {
    return undefined;
  }
}

// rememberReferrer keeps the first landing's referrer of the tab, none included: the identity provider's
// return is a new page load whose referrer is the provider, and it must never pass for where the person came from.
function rememberReferrer(referrer: string, ownHost: string): void {
  if (readReferrer() !== undefined) return;
  try {
    window.sessionStorage.setItem(referrerKey, JSON.stringify({ host: referrerHost(referrer, ownHost), at: Date.now() }));
  } catch {
    // Storage can be blocked; the trial then starts without the referring site.
  }
}

// rememberSignupAttribution keeps the attribution of a sign-in address. An address without any keeps what
// an earlier landing in this tab remembered; one with some replaces it. A click ID already remembered keeps
// the time it was first seen, so the identity provider's return is not a second click.
export function rememberSignupAttribution(search: string, referrer = document.referrer, ownHost = window.location.hostname): void {
  rememberReferrer(referrer, ownHost);
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
  const referrer = readReferrer();
  if (!stored && !referrer) return undefined;
  return {
    ...stored?.values,
    ...(stored?.clickSeenAt !== undefined ? { click_id_seen_at: new Date(stored.clickSeenAt).toISOString() } : {}),
    ...(referrer ? { referrer } : {}),
  };
}

export function clearSignupAttribution(): void {
  try {
    window.sessionStorage.removeItem(storageKey);
    window.sessionStorage.removeItem(referrerKey);
  } catch {
    // Nothing was stored.
  }
}
