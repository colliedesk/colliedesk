import type { Message } from '../../api/schemas';
import { maxCc } from './cc-field';

// suggestedCc lists who to copy on a reply by default: the people on the Cc line of the customer's
// latest message, and that message's sender when it is not the person who opened the conversation (a
// copied colleague who answered with reply-all). The person who opened the conversation already gets
// the reply in To. Older mail has no Cc, so it offers nothing.
export function suggestedCc(timeline: Message[]): string[] {
  const received = timeline.filter((item) => item.kind === 'incoming' && item.email);
  const latest = received.at(-1)?.email;
  if (!latest) return [];
  const opener = received[0]?.email?.from.toLowerCase();
  const offered: string[] = [];
  for (const address of [latest.from, ...(latest.cc ?? [])].map((item) => item.toLowerCase())) {
    if (address !== opener && !offered.includes(address)) offered.push(address);
  }
  return offered.slice(0, maxCc);
}
