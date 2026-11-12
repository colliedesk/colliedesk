import './mentions.css';
import { useId } from 'react';
import type { TeamMember } from '../../api/schemas';
import { initials } from '../../ui/avatar';
import { mentionMessages } from './mention-messages';

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

// mentionedIn lists the teammates whose @Name the text holds, each once.
export function mentionedIn(text: string, teammates: Teammate[]): string[] {
  return [...new Set(teammates.filter((person) => text.includes(`@${person.name}`)).map((person) => person.id))];
}

interface MentionPickerProps {
  options: Teammate[];
  query: string;
  active: number;
  onPick: (person: Teammate) => void;
  onActive: (index: number) => void;
  // The list's id, for the box's aria-controls and aria-activedescendant.
  id: string;
}

export const optionIdOf = (list: string, index: number) => `${list}-option-${index}`;

// MentionPicker is the list under the note box while an @ is typed: the box keeps the focus and its keys (arrows,
// Enter, Esc) move in the list, so writing never stops.
export function MentionPicker({ options, query, active, onPick, onActive, id }: MentionPickerProps) {
  const label = useId();
  return (
    <div className="mention-picker">
      <p id={label} className="mention-picker-label">{mentionMessages.pickerLabel}</p>
      {options.length === 0 ? <p className="mention-picker-empty" role="status">{mentionMessages.noTeammateMatches}</p>
        : <ul id={id} role="listbox" aria-labelledby={label} onMouseDown={(event) => event.preventDefault()}>
          {options.map((person, index) => {
            const at = person.name.toLocaleLowerCase().indexOf(query.toLocaleLowerCase());
            const name = query && at >= 0 ? <><span>{person.name.slice(0, at)}</span><strong>{person.name.slice(at, at + query.length)}</strong><span>{person.name.slice(at + query.length)}</span></> : person.name;
            return <li key={person.id} id={optionIdOf(id, index)} role="option" aria-selected={index === active}
              onMouseMove={() => { if (index !== active) onActive(index); }} onClick={() => onPick(person)}>
              <span className="mention-initials" aria-hidden="true">{initials(person.name)}</span>
              <span className="mention-name">{name}</span>
              {person.role && <span className="mention-role">{person.role}</span>}
            </li>;
          })}
        </ul>}
    </div>
  );
}
