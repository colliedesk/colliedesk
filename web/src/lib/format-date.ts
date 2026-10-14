function format(value: Date, timeZone: string, options: Intl.DateTimeFormatOptions): string {
  try {
    return new Intl.DateTimeFormat('en', { ...options, timeZone }).format(value);
  } catch {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' }).format(value);
  }
}

export function formatDate(value: string, timeZone: string): string {
  return format(new Date(value), timeZone, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
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
