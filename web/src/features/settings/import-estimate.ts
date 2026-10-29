import type { ImportPreview } from '../../api/imports';

// A ticket costs the ticket itself and its conversations; the account's time entries are listed once.
const requestsPerTicket = 2.4;
// Of the requests per minute a plan allows, the import gets about 45% on a slow stretch (it pauses when
// Freshdesk answers "too many") and 80% on a good one (a fifth is left to the account's other apps).
const slowShare = 0.45;
const fastShare = 0.8;
// Without a rate limit read from Freshdesk, plans allow between these requests a minute (trials 50, Growth 100).
const unknownRate = { low: 50, high: 100 };

// estimateRange is "12 to 21 minutes" for the tickets still to read, or undefined when there are none to size.
export function estimateRange(preview: ImportPreview): string | undefined {
  const tickets = Math.max(preview.tickets_outside_archive ?? 0, preview.highest_listed_ticket_id);
  if (tickets === 0) return undefined;
  const known = preview.rate_limit_per_minute > 0;
  const requests = tickets * requestsPerTicket;
  const low = Math.ceil(requests / ((known ? preview.rate_limit_per_minute : unknownRate.high) * fastShare));
  const high = Math.ceil(requests / ((known ? preview.rate_limit_per_minute : unknownRate.low) * slowShare));
  if (high < 90) return span(low, high, 'minute');
  return span(Math.ceil(low / 60), Math.ceil(high / 60), 'hour');
}

function span(low: number, high: number, unit: string): string {
  const plural = (n: number) => `${n} ${unit}${n === 1 ? '' : 's'}`;
  return low === high ? plural(high) : `${low} to ${plural(high)}`;
}
