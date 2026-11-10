// A response target on the page: a number and a unit; on the wire it is whole business minutes.

export const units = ['min', 'h'] as const;
export type Unit = (typeof units)[number];
export interface Duration { amount: string; unit: Unit }

// Shown the way people say it, and always exact: whole hours (0 too) in hours, anything else in minutes.
// 15 is 15 min, 60 is 1 h, 480 is 8 h, 90 is 90 min (not 1.5 h). An empty box starts in hours.
export const durationOf = (minutes: number | null): Duration => (minutes === null ? { amount: '', unit: 'h' }
  : minutes % 60 === 0 ? { amount: String(minutes / 60), unit: 'h' } : { amount: String(minutes), unit: 'min' });

export const spoken = (minutes: number) => { const { amount, unit } = durationOf(minutes); return `${amount} ${unit}`; };

// The minutes typed: null for an empty box, undefined when it is no whole number of minutes (1.5 min, 0.01 h).
export function minutesOf({ amount, unit }: Duration): number | null | undefined {
  if (amount.trim() === '') return null;
  const minutes = Number(amount) * (unit === 'h' ? 60 : 1);
  const whole = Math.round(minutes);
  // 1.1 h is 66.00000000000001 in floating point: that is 66 minutes.
  return Number.isFinite(minutes) && Math.abs(minutes - whole) < 1e-6 ? whole : undefined;
}

// A target takes 1 minute or more, or none; due soon takes 0 minutes or more.
export const targetOK = (duration: Duration) => { const minutes = minutesOf(duration); return minutes === null || (minutes !== undefined && minutes >= 1); };
export const warnOK = (duration: Duration) => { const minutes = minutesOf(duration); return typeof minutes === 'number' && minutes >= 0; };
