// Durations and changes as the results page writes them (docs/user/results.md).

// formatDuration writes seconds as minutes under an hour, hours and minutes under a day, days and
// hours from there.
export function formatDuration(seconds: number): string {
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  if (minutes < 24 * 60) {
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    return rest === 0 ? `${hours} h` : `${hours} h ${String(rest).padStart(2, '0')} min`;
  }
  const hours = Math.round(minutes / 60);
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return rest === 0 ? `${days} d` : `${days} d ${rest} h`;
}

export interface Change {
  // direction is better or worse for the team; undefined when it is neither (volume).
  direction?: 'better' | 'worse';
  headline: string;
  detail: string;
}

// timeChange compares two durations: less time is better.
export function timeChange(before: number, after: number): Change | undefined {
  if (before <= 0) return undefined;
  const difference = after - before;
  const percent = Math.round(Math.abs(difference) / before * 100);
  if (Math.round(difference / 60) === 0) return { headline: 'About the same', detail: 'Less than a minute apart' };
  return difference < 0
    ? { direction: 'better', headline: `${formatDuration(-difference)} faster`, detail: `${percent}% less time` }
    : { direction: 'worse', headline: `${formatDuration(difference)} slower`, detail: `${percent}% more time` };
}

// volumeChange compares conversations a week: more or fewer is neither better nor worse.
export function volumeChange(before: number, after: number): Change | undefined {
  if (before <= 0) return undefined;
  const difference = after - before;
  const percent = Math.round(Math.abs(difference) / before * 100);
  const weekly = Math.round(Math.abs(difference));
  if (percent === 0) return { headline: 'About the same', detail: `${weekly} a week apart` };
  return difference < 0 ? { headline: `${percent}% fewer`, detail: `${weekly} fewer a week` } : { headline: `${percent}% more`, detail: `${weekly} more a week` };
}

// perWeek writes a weekly figure without a needless decimal.
export function perWeek(value: number): string {
  return Number.isInteger(value) || value >= 10 ? String(Math.round(value)) : value.toFixed(1);
}

export function weeks(days: number): string {
  const value = days / 7;
  return value >= 10 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '');
}
