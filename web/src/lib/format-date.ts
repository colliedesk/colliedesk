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
