import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { SavedReply } from '../../api/schemas';
import { savedReplyMatches } from '../../lib/saved-reply-variables';
import { messages } from '../../messages';

interface SavedReplyPickerProps {
  replies: SavedReply[];
  onPick: (reply: SavedReply) => void;
  // restoreFocus is true when the person dismissed it with Escape, false when focus moved elsewhere on its own.
  onClose: (restoreFocus: boolean) => void;
  // Leads to the place that adds saved replies; shown when there are none.
  onManage?: () => void;
}

// SavedReplyPicker is a searchable list the keyboard drives end to end: type to filter by title,
// arrows to move, Enter to insert, Escape to close. The list is already in memory, so it opens at once.
export function SavedReplyPicker({ replies, onPick, onClose, onManage }: SavedReplyPickerProps) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const search = useRef<HTMLInputElement>(null);
  const prefix = useId();
  const matches = useMemo(() => replies.filter((reply) => savedReplyMatches(reply.title, query)), [replies, query]);
  const current = Math.min(active, matches.length - 1);
  useEffect(() => search.current?.focus(), []);
  const onKey = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === 'Escape') { event.preventDefault(); onClose(true); }
    else if (event.key === 'ArrowDown') { event.preventDefault(); setActive(Math.min(current + 1, matches.length - 1)); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActive(Math.max(current - 1, 0)); }
    else if (event.key === 'Enter') { event.preventDefault(); if (matches[current]) onPick(matches[current]); }
  };
  const optionId = (index: number) => `${prefix}-option-${index}`;
  return (
    <div className="saved-reply-picker" role="dialog" aria-label={messages.savedReplies}
      // Focus leaving the panel (a click elsewhere) closes it; a click inside keeps focus in the search box.
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) onClose(false); }}>
      <input ref={search} type="text" role="combobox" aria-expanded="true" aria-controls={`${prefix}-list`} aria-autocomplete="list"
        aria-activedescendant={matches.length > 0 ? optionId(current) : undefined} aria-label={messages.searchSavedReplies}
        placeholder={messages.searchSavedReplies} autoComplete="off" value={query} onKeyDown={onKey}
        onChange={(event) => { setQuery(event.target.value); setActive(0); }} />
      <ul id={`${prefix}-list`} role="listbox" aria-label={messages.savedReplies} onMouseDown={(event) => event.preventDefault()}>
        {matches.map((reply, index) => (
          <li key={reply.id} id={optionId(index)} role="option" aria-selected={index === current} onClick={() => onPick(reply)}
            onMouseMove={() => { if (index !== current) setActive(index); }}>{reply.title}</li>
        ))}
      </ul>
      {matches.length === 0 && <p className="saved-reply-empty" role="status">{replies.length === 0 ? messages.noSavedReplies : messages.noSavedRepliesMatch}</p>}
      {replies.length === 0 && onManage && <button type="button" onClick={onManage}>{messages.manageSavedReplies}</button>}
    </div>
  );
}
