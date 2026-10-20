import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { SavedReply } from '../../api/schemas';
import { savedReplyMatches } from '../../lib/saved-reply-variables';
import { messages } from '../../messages';

// The names the picker shows: what the list is, its search box, its four states and the way to add some.
export interface PickerTexts { label: string; search: string; loading: string; failed: string; none: string; noMatch: string; manage: string }

const savedReplyTexts: PickerTexts = {
  label: messages.savedReplies, search: messages.searchSavedReplies, loading: messages.loadingSavedReplies,
  failed: messages.savedRepliesFailed, none: messages.noSavedReplies, noMatch: messages.noSavedRepliesMatch, manage: messages.manageSavedReplies,
};

interface SavedReplyPickerProps<T extends { id: string; title: string }> {
  replies: T[];
  onPick: (reply: T) => void;
  // restoreFocus is true when the person dismissed it with Escape, false when focus moved elsewhere on its own.
  onClose: (restoreFocus: boolean) => void;
  // Leads to the place that adds saved replies (texts.manage); shown when there are none.
  onManage?: () => void;
  // The list comes from a query: while it loads or after it failed, "none yet" would be untrue.
  state?: 'loading' | 'failed' | 'ready';
  // Another source than saved replies names itself (articles); the default is saved replies.
  texts?: PickerTexts;
  // A source that searches by itself (the server) gets each search and hands back what matches, so the
  // picker shows its list as it is instead of matching titles in memory.
  onQueryChange?: (query: string) => void;
}

// SavedReplyPicker is a searchable list the keyboard drives end to end: type to filter by title,
// arrows to move, Enter to insert, Escape to close. The list is already in memory, so it opens at once.
export function SavedReplyPicker<T extends { id: string; title: string } = SavedReply>({ replies, onPick, onClose, onManage, state = 'ready', texts = savedReplyTexts, onQueryChange }: SavedReplyPickerProps<T>) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const search = useRef<HTMLInputElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const prefix = useId();
  const matches = useMemo(() => (onQueryChange ? replies : replies.filter((reply) => savedReplyMatches(reply.title, query))), [replies, query, onQueryChange]);
  const current = Math.min(active, matches.length - 1);
  // Without a search, an empty list means there is nothing at all, not nothing that matches.
  const nothing = onQueryChange ? query.trim() === '' : replies.length === 0;
  // On a phone the list can open below the fold; it comes into view with the search box.
  useEffect(() => { search.current?.focus(); panel.current?.scrollIntoView?.({ block: 'nearest' }); }, []);
  const onKey = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === 'Escape') { event.preventDefault(); onClose(true); }
    else if (event.key === 'ArrowDown') { event.preventDefault(); setActive(Math.min(current + 1, matches.length - 1)); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActive(Math.max(current - 1, 0)); }
    else if (event.key === 'Enter') { event.preventDefault(); if (matches[current]) onPick(matches[current]); }
  };
  const optionId = (index: number) => `${prefix}-option-${index}`;
  return (
    <div ref={panel} className="saved-reply-picker" role="dialog" aria-label={texts.label}
      // Focus leaving the panel (a click elsewhere) closes it; a click inside keeps focus in the search box.
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) onClose(false); }}>
      <input ref={search} type="text" role="combobox" aria-expanded="true" aria-controls={`${prefix}-list`} aria-autocomplete="list"
        aria-activedescendant={matches.length > 0 ? optionId(current) : undefined} aria-label={texts.search}
        placeholder={texts.search} autoComplete="off" value={query} onKeyDown={onKey}
        onChange={(event) => { setQuery(event.target.value); setActive(0); onQueryChange?.(event.target.value); }} />
      <ul id={`${prefix}-list`} role="listbox" aria-label={texts.label} onMouseDown={(event) => event.preventDefault()}>
        {matches.map((reply, index) => (
          <li key={reply.id} id={optionId(index)} role="option" aria-selected={index === current} onClick={() => onPick(reply)}
            onMouseMove={() => { if (index !== current) setActive(index); }}>{reply.title}</li>
        ))}
      </ul>
      {state === 'loading' && replies.length === 0 && <p className="saved-reply-empty" role="status">{texts.loading}</p>}
      {state === 'failed' && replies.length === 0 && <p className="saved-reply-empty" role="alert">{texts.failed}</p>}
      {state === 'ready' && matches.length === 0 && <p className="saved-reply-empty" role="status">{nothing ? texts.none : texts.noMatch}</p>}
      {state === 'ready' && replies.length === 0 && nothing && onManage && <button type="button" onClick={onManage}>{texts.manage}</button>}
    </div>
  );
}
