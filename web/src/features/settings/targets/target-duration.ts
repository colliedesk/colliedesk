// A response target on the page: a number and a unit; on the wire it is whole business minutes.

export const units = ['min', 'h'] as const;
export type Unit = (typeof units)[number];
export interface Duration { amount: string; unit: Unit }

// The server's limits: a target is at most a year of business minutes, due soon at most a week.
export const maxTargetMinutes = 525600;
export const maxWarnMinutes = 10080;

// Shown the way people say it, and always exact: whole and half hours (0 too) in hours, anything else in
// minutes. 15 is 15 min, 45 is 45 min, 60 is 1 h, 90 is 1.5 h, 510 is 8.5 h. An empty box starts in hours.
export const durationOf = (minutes: number | null): Duration => (minutes === null ? { amount: '', unit: 'h' }
  : minutes % 30 === 0 ? { amount: String(minutes / 60), unit: 'h' } : { amount: String(minutes), unit: 'min' });

export const spoken = (minutes: number) => { const { amount, unit } = durationOf(minutes); return `${amount} ${unit}`; };

// The minutes typed: null for an empty box, undefined when it is no whole number of minutes (1.5 min, 0.01 h).
export function minutesOf({ amount, unit }: Duration): number | null | undefined {
  if (amount.trim() === '') return null;
  const minutes = Number(amount) * (unit === 'h' ? 60 : 1);
  const whole = Math.round(minutes);
  // 1.1 h is 66.00000000000001 in floating point: that is 66 minutes. 1e20 is no safe integer: refused as too long.
  if (!Number.isFinite(minutes) || Math.abs(minutes - whole) > 1e-6) return undefined;
  return Number.isSafeInteger(whole) ? whole : Number.MAX_SAFE_INTEGER;
}

// What is wrong with a box: 'rule' (below the least, or a part of a minute), 'max' (above the limit), or nothing.
export type Problem = 'rule' | 'max' | undefined;
const check = (duration: Duration, least: number, most: number, emptyOK: boolean): Problem => {
  const minutes = minutesOf(duration);
  if (minutes === null) return emptyOK ? undefined : 'rule';
  if (minutes === undefined || minutes < least) return 'rule';
  return minutes > most ? 'max' : undefined;
};
// A target takes 1 minute to a year, or none; due soon takes 0 minutes to a week.
export const targetProblem = (duration: Duration) => check(duration, 1, maxTargetMinutes, true);
export const warnProblem = (duration: Duration) => check(duration, 0, maxWarnMinutes, false);

// A limit in the unit a box shows: 525600 is "8760 h" in hours, "525600 min" in minutes.
export const limitIn = (minutes: number, unit: Unit) => (unit === 'h' ? `${minutes / 60} h` : `${minutes} min`);
