import './mentions.css';
import { useId } from 'react';
import type { CSSProperties, Ref, RefObject } from 'react';
import { createPortal } from 'react-dom';
import { initials } from '../../ui/avatar';
import { useCaretAnchor } from './caret-anchor';
import { mentionMessages } from './mention-messages';
import { optionIdOf } from './mention-match';
import type { Teammate } from './mention-match';

interface MentionPickerProps {
  options: Teammate[];
  query: string;
  active: number;
  onPick: (person: Teammate) => void;
  onActive: (index: number) => void;
  // The list's id, for the box's aria-controls and aria-activedescendant.
  id: string;
  // Where it floats: the composer anchors it at the @ (useCaretAnchor's ref and style).
  ref?: Ref<HTMLDivElement>;
  style?: CSSProperties;
}

// MentionPicker is the list at the @ of a note while it is typed: the box keeps the focus and its keys (arrows,
// Enter, Esc) move in the list, so writing never stops.
export function MentionPicker({ options, query, active, onPick, onActive, id, ref, style }: MentionPickerProps) {
  const label = useId();
  return (
    <div className="mention-picker" ref={ref} style={style}>
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

// MentionPopup floats the list at the @ of the box, like a chat app's: under its line from the @ on, above the line when
// the screen has no room under it, and never past an edge of the screen. It lives on the page's body, so a reply box that
// scrolls never cuts it.
export function MentionPopup({ box, at, ...picker }: Omit<MentionPickerProps, 'ref' | 'style' | 'query'> & { box: RefObject<HTMLTextAreaElement | null>; at: { start: number; query: string } }) {
  const anchor = useCaretAnchor(box, at);
  return createPortal(<MentionPicker {...picker} query={at.query} ref={anchor.ref} style={anchor.style} />, document.body);
}
