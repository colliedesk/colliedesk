import { messages } from '../messages';

// formatWait turns a wait in seconds into "N minutes" or "N hours", never less than a minute.
export function formatWait(seconds: number): string {
  const minutes = Math.max(1, Math.ceil(seconds / 60));
  return minutes < 90 ? messages.minutes(minutes) : messages.hours(Math.ceil(minutes / 60));
}
