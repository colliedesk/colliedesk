function format(value: Date, timeZone: string, options: Intl.DateTimeFormatOptions): string {
  try {
    return new Intl.DateTimeFormat('en', { ...options, timeZone }).format(value);
  } catch {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' }).format(value);
  }
}

const dateTime: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' };

export function formatDate(value: string, timeZone: string, now: Date = new Date()): string {
  const date = new Date(value);
  const year = (input: Date) => format(input, timeZone, { year: 'numeric' });
  // Imported history spans years; a date without its year would be ambiguous.
  return format(date, timeZone, year(date) === year(now) ? dateTime : { ...dateTime, year: 'numeric' });
}

// formatDay is a calendar day with its year, for dates where the time of day means nothing (a trial
// end, a renewal).
export function formatDay(value: string, timeZone: string): string {
  return format(new Date(value), timeZone, { year: 'numeric', month: 'short', day: 'numeric' });
}

// formatMonthDay is a day of the near future ("Nov 13"), such as a trial end; it carries the year only when
// that is not this one.
export function formatMonthDay(value: string, timeZone: string, now: Date = new Date()): string {
  const date = new Date(value);
  const year = (input: Date) => format(input, timeZone, { year: 'numeric' });
  return format(date, timeZone, year(date) === year(now) ? { month: 'short', day: 'numeric' } : { year: 'numeric', month: 'short', day: 'numeric' });
}

// Compact form for dense lists: the time for today, the day otherwise, the year only when it differs.
// ICU may separate "1:05" and "PM" with a narrow no-break space; plain spaces keep it predictable.
export function formatShortDate(value: string, timeZone: string, now = new Date()): string {
  const date = new Date(value);
  const day = (input: Date) => format(input, timeZone, { year: 'numeric', month: 'short', day: 'numeric' });
  const options: Intl.DateTimeFormatOptions = day(date) === day(now) ? { hour: 'numeric', minute: '2-digit' }
    : format(date, timeZone, { year: 'numeric' }) === format(now, timeZone, { year: 'numeric' }) ? { month: 'short', day: 'numeric' }
      : { year: 'numeric', month: 'short', day: 'numeric' };
  return format(date, timeZone, options).replace(/\s/g, ' ');
}

// Lists are ordered by latest activity, so a day boundary splits them into two contiguous groups.
export function dayGroup(value: string, timeZone: string, now = new Date()): 'today' | 'earlier' {
  const day = (input: Date) => format(input, timeZone, { year: 'numeric', month: 'numeric', day: 'numeric' });
  return day(new Date(value)) === day(now) ? 'today' : 'earlier';
}

// formatListTime is a list row's stamp where no tooltip can show more (a touch screen): the time alone
// for today, under the Today heading, and the full date and time for anything earlier. The day is the
// calendar day in the agent's timezone, the same one the Today and Earlier groups use.
export function formatListTime(value: string, timeZone: string, now = new Date()): string {
  const label = dayGroup(value, timeZone, now) === 'today' ? format(new Date(value), timeZone, { hour: 'numeric', minute: '2-digit' })
    : formatDate(value, timeZone, now);
  return label.replace(/\s/g, ' ');
}

// The browser's own timezone, which the person reads their clock in.
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

// One formatter per zone: a list formats many rows, and making a formatter is the slow part.
const zoneFormats = new Map<string, Intl.DateTimeFormat>();

// The short name of a timezone at a moment: UTC, GMT+7, EDT.
function zoneName(date: Date, timeZone: string): string {
  const part = (zone: string) => {
    let formatter = zoneFormats.get(zone);
    if (!formatter) {
      formatter = new Intl.DateTimeFormat('en', { timeZone: zone, timeZoneName: 'short' });
      zoneFormats.set(zone, formatter);
    }
    return formatter.formatToParts(date).find((item) => item.type === 'timeZoneName')?.value ?? zone;
  };
  try {
    return part(timeZone);
  } catch {
    return part('UTC');
  }
}

// zoneSuffix names the timezone after a stamp (" UTC") only when the agent reads times in another zone
// than the browser's clock at that moment, so a time that does not match the clock on the wall says why.
export function zoneSuffix(value: string, timeZone: string, browser = browserTimeZone()): string {
  const date = new Date(value);
  const shown = zoneName(date, timeZone);
  return shown === zoneName(date, browser) ? '' : ` ${shown}`;
}

// stampTitle is the full date and time with its timezone, for the title of a shorter stamp.
export function stampTitle(value: string, timeZone: string): string {
  const date = new Date(value);
  return `${format(date, timeZone, { year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })} ${zoneName(date, timeZone)}`.replace(/\s/g, ' ');
}
