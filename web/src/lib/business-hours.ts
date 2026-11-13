// Business hours as the setup flow and Settings → Response targets both say them, and the hours a turn-on
// starts with. The server names the hours a workspace without any starts with (reply_target_hours of the
// setup progress); the two screens read them from there, so they never disagree.

export interface HoursDay { day: string; open: boolean; from?: string; to?: string }

const names: Record<string, string> = { mon: 'Monday', tue: 'Tuesday', wed: 'Wednesday', thu: 'Thursday', fri: 'Friday', sat: 'Saturday', sun: 'Sunday' };
const order = Object.keys(names);

// The page shows a day that closes at midnight as 00:00.
const clock = (value: string) => (value === '24:00' ? '00:00' : value);

// hoursText writes the open days as runs of days with the same hours: "Monday to Friday, 09:00 to 18:00".
export function hoursText(hours: HoursDay[]): string {
  const open = order.flatMap((key) => {
    const item = hours.find((day) => day.day === key);
    return item?.open && item.from && item.to ? [{ key, span: `${clock(item.from)} to ${clock(item.to)}` }] : [];
  });
  if (open.length === 0) return 'closed every day';
  const runs: { first: string; last: string; count: number; span: string }[] = [];
  for (const { key, span } of open) {
    const run = runs.at(-1);
    if (run && run.span === span && order.indexOf(run.last) === order.indexOf(key) - 1) { run.last = key; run.count += 1; } else runs.push({ first: key, last: key, count: 1, span });
  }
  return runs.map(({ first, last, count, span }) => {
    const days = count === 1 ? names[first] : count === 2 ? `${names[first]} and ${names[last]}` : `${names[first]} to ${names[last]}`;
    return `${days}, ${span}`;
  }).join('; ');
}

// startingHours is what the clock runs on when measuring starts: the hours saved earlier when some day is open
// (they are never replaced), else the fallback the server names. kept says which.
export function startingHours<S extends HoursDay, F extends HoursDay>(saved: S[], fallback: F[]): { hours: S[] | F[]; kept: boolean } {
  return saved.some((day) => day.open) ? { hours: saved, kept: true } : { hours: fallback, kept: false };
}
