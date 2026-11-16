import type { TeamMember } from '../../api/schemas';

// Who a private note asks: the teammates it can ask, the @ being typed, and the whole @Names the text holds. The list
// itself (mention-picker.tsx) loads with the reply pickers' chunk; these stay with the composer, which needs them on every key.

// A teammate a private note can ask: the name the note shows after @, and what the list says beside it.
export interface Teammate { id: string; name: string; email: string; role: string }

// askableTeammates are the people a note can ask: active members of the workspace other than the writer. Customers
// are never members, so they are never listed.
export function askableTeammates(members: TeamMember[], me?: string): Teammate[] {
  return members.filter((member) => !member.deactivated_at && member.id !== me)
    .map((member) => ({ id: member.id, name: member.display_name.trim() || member.email, email: member.email, role: member.roles[0]?.name ?? '' }));
}

// mentionAt finds the @ the caret is typing after: where it starts and what follows it, or undefined when the caret
// is not in one (an @ starts a word; the query stops at a space or a new line).
export function mentionAt(text: string, caret: number): { start: number; query: string } | undefined {
  const match = /(^|\s)@([^\s@]{0,40})$/.exec(text.slice(0, caret));
  return match ? { start: caret - match[2].length - 1, query: match[2] } : undefined;
}

// matchingTeammates are the teammates whose name, or a word of it, or whose email starts with the query.
export function matchingTeammates(teammates: Teammate[], query: string): Teammate[] {
  const wanted = query.toLocaleLowerCase();
  return teammates.filter((person) => [person.name, ...person.name.split(/\s+/), person.email]
    .some((part) => part.toLocaleLowerCase().startsWith(wanted))).slice(0, 8);
}

// A name ends where no letter, mark or digit follows it, so "@Long" is not read inside "@Longer".
const nameEnds = (text: string, at: number) => at >= text.length || !/[\p{L}\p{M}\p{N}]/u.test(text[at]);

// mentionedIn lists the teammates whose whole @Name the text holds, each once, in the order they are asked. At each @
// the longest name that fits wins: "@Long Bui" asks Long Bui, never a teammate named Long as well. Teammates who share
// that name are all asked, since the text cannot tell them apart. Text and names are compared in one Unicode form
// (NFC), so an accent typed as a separate mark ("u" + U+0300) still reads as "ù".
export function mentionedIn(text: string, teammates: Teammate[]): string[] {
  const written = text.normalize('NFC');
  const byLength = teammates.map((person) => ({ id: person.id, name: person.name.normalize('NFC') }))
    .sort((a, b) => b.name.length - a.name.length);
  const asked: string[] = [];
  for (let at = written.indexOf('@'); at !== -1; at = written.indexOf('@', at + 1)) {
    const fits = byLength.filter((each) => written.startsWith(each.name, at + 1) && nameEnds(written, at + 1 + each.name.length));
    asked.push(...fits.filter((each) => each.name.length === fits[0].name.length).map((each) => each.id));
  }
  return [...new Set(asked)];
}

export const optionIdOf = (list: string, index: number) => `${list}-option-${index}`;
