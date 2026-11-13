// Dates of the results page, written in the agent's timezone.

const dayFormat = (timezone: string, withYear: boolean) => {
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', ...(withYear ? { year: 'numeric' } : {}) };
  try {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: timezone });
  } catch {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' });
  }
};

export function day(value: string, timezone: string, withYear = false): string {
  return dayFormat(timezone, withYear).format(new Date(value));
}

// range writes a window's days; the year only once, at the end.
export function range(from: string, to: string, timezone: string, withYear = false): string {
  return `${day(from, timezone)} – ${day(to, timezone, withYear)}`;
}

export function ago(value: string, now: Date): string {
  const minutes = Math.max(0, Math.round((now.getTime() - new Date(value).getTime()) / 60000));
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  return hours < 24 ? `${hours} h ago` : `${Math.round(hours / 24)} d ago`;
}
