const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' };

function format(value: Date, timeZone: string, now: Date): string {
  const year = (date: Date) => new Intl.DateTimeFormat('en', { year: 'numeric', timeZone }).format(date);
  // Imported history spans years; a date without its year would be ambiguous.
  const withYear = year(value) === year(now) ? options : { ...options, year: 'numeric' as const };
  return new Intl.DateTimeFormat('en', { ...withYear, timeZone }).format(value);
}

export function formatDate(value: string, timeZone: string, now: Date = new Date()): string {
  try {
    return format(new Date(value), timeZone, now);
  } catch {
    return format(new Date(value), 'UTC', now);
  }
}
