import type { Message } from '../../api/schemas';
import { maxCc } from './cc-field';

// suggestedCc lists who to copy on a reply by default: the people on the Cc line of the customer's
// latest message, and that message's sender when it is not the person who opened the conversation (a
// copied colleague who answered with reply-all). The person who opened the conversation already gets
// the reply in To. Older mail has no Cc, so it offers nothing. The sender writes the Cc line, so mail
// whose authentication did not pass offers nothing either: it would prefill whoever a forger named.
// On the hosted cloud the server trusts a Cc line only on mail from the platform forwarder; mail posted
// to the workspace's own inbound address is written by its admin, so it offers nothing there.
export function suggestedCc(timeline: Message[], { hosted = false }: { hosted?: boolean } = {}): string[] {
  const received = timeline.filter((item) => item.kind === 'incoming' && item.email);
  const latest = received.at(-1)?.email;
  if (!latest || latest.authentication !== 'pass') return [];
  if (hosted && latest.via !== 'platform') return [];
  const opener = received[0]?.email?.from.toLowerCase();
  const offered: string[] = [];
  for (const address of [latest.from, ...(latest.cc ?? [])].map((item) => item.toLowerCase())) {
    if (address !== opener && !offered.includes(address)) offered.push(address);
  }
  return offered.slice(0, maxCc);
}
