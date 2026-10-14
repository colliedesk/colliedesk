export function formatDate(value: string, timeZone: string): string {
  const options: Intl.DateTimeFormatOptions = {
    month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit',
  };
  try {
    return new Intl.DateTimeFormat('en', { ...options, timeZone }).format(new Date(value));
  } catch {
    return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' }).format(new Date(value));
  }
}
