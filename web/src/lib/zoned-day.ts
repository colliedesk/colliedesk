// Calendar days in an IANA timezone, for the inbox date filters. Kept apart from format-date.ts so they
// load with the Filter menu, not with the first screen.

// dayParts reads the calendar fields of an instant in a timezone (UTC when the zone is unknown).
function dayParts(value: Date, timeZone: string): Record<string, number> {
  const options: Intl.DateTimeFormatOptions = { hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric' };
  let formatter: Intl.DateTimeFormat;
  try {
    formatter = new Intl.DateTimeFormat('en-US', { ...options, timeZone });
  } catch {
    formatter = new Intl.DateTimeFormat('en-US', { ...options, timeZone: 'UTC' });
  }
  return Object.fromEntries(formatter.formatToParts(value).filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]));
}

// zonedDay is the calendar day (YYYY-MM-DD) an instant falls on in the timezone; empty for a bad instant.
export function zonedDay(value: string, timeZone: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const parts = dayParts(date, timeZone);
  return `${String(parts.year).padStart(4, '0')}-${String(parts.month).padStart(2, '0')}-${String(parts.day).padStart(2, '0')}`;
}

// zonedDayStart is the UTC instant (ISO 8601) at which a calendar day (YYYY-MM-DD) begins in the timezone.
// The offset is read twice, so a day that begins next to a daylight saving switch still lands on its start.
export function zonedDayStart(day: string, timeZone: string): string | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!match) return undefined;
  const wall = Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  if (Number.isNaN(wall)) return undefined;
  const offsetAt = (instant: number) => {
    const parts = dayParts(new Date(instant), timeZone);
    return Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second) - instant;
  };
  const first = wall - offsetAt(wall);
  let instant = wall - offsetAt(first);
  // Where a switch skips midnight (Santiago, Havana), that instant still falls on the day before: the day
  // starts at the first quarter hour that belongs to it.
  for (let step = 0; step < 12 && zonedDay(new Date(instant).toISOString(), timeZone) !== day; step++) instant += 15 * 60 * 1000;
  return new Date(instant).toISOString();
}
