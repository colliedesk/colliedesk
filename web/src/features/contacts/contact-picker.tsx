import { useEffect, useId, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { contactApi } from './contact-api';
import type { Contact } from '../../api/schemas';
import { messages } from '../../messages';
import { contactAddress, contactMessages } from './contact-messages';

// How long the text stays unchanged before it is searched: short enough that results follow the typing
// within the 200 ms budget, long enough that a fast typist sends one request per pause.
export const searchSettleMs = 100;
// A picker shows a short list; the person narrows the text instead of scrolling.
export const pickerLimit = 8;

interface ContactPickerProps {
  label: string;
  onPick: (contact: Contact) => void;
  // A contact never offered, for example the one a duplicate is merged into.
  excludeId?: string;
  autoFocus?: boolean;
  disabled?: boolean;
  // Shown under the results when nothing matches, for example a way to add the customer.
  emptyText?: string;
}

// ContactPicker finds a contact by name or email: a combobox whose matches open under the box like a
// select, driven by the keyboard (arrows to move, Enter to pick, Escape to close). Merged contacts are
// never offered: the server leaves them out of a search.
export function ContactPicker({ label, onPick, excludeId, autoFocus, disabled, emptyText = contactMessages.mergeSearchEmpty }: ContactPickerProps) {
  const [text, setText] = useState('');
  const [term, setTerm] = useState('');
  const [open, setOpen] = useState(true);
  const [active, setActive] = useState(0);
  const inputId = useId();
  const listId = useId();
  useEffect(() => {
    const timer = setTimeout(() => setTerm(text.trim()), searchSettleMs);
    return () => clearTimeout(timer);
  }, [text]);
  const found = useQuery({ queryKey: ['contacts', 'search', term], queryFn: ({ signal }) => contactApi.searchContacts(term, pickerLimit + 1, signal),
    enabled: term !== '', placeholderData: keepPreviousData });
  const items = (found.data?.items ?? []).filter((contact) => contact.id !== excludeId).slice(0, pickerLimit);
  const settled = term !== '' && found.isSuccess && !found.isPlaceholderData;
  const shown = open && term !== '' && text.trim() !== '' && items.length > 0;
  const current = Math.max(0, Math.min(active, items.length - 1));
  // Enter picks only from the answer to what is typed now, never from the list of an earlier search.
  const answered = settled && term === text.trim();
  const optionId = (index: number) => `${listId}-option-${index}`;
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === 'ArrowDown') { event.preventDefault(); setOpen(true); if (items.length > 0) setActive(Math.min(current + 1, items.length - 1)); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActive(Math.max(current - 1, 0)); }
    else if (event.key === 'Enter' && shown) { event.preventDefault(); if (answered && items[current]) onPick(items[current]); }
    // Escape closes an open list first; a closed one lets the window handle Escape.
    else if (event.key === 'Escape' && shown) { event.preventDefault(); event.stopPropagation(); setOpen(false); }
  };
  return (
    <div className="contact-picker">
      <label htmlFor={inputId}>{label}</label>
      <div className="contact-picker-box">
        <input id={inputId} type="search" role="combobox" value={text} autoFocus={autoFocus} disabled={disabled} autoComplete="off"
          aria-controls={listId} aria-expanded={shown} aria-autocomplete="list" aria-activedescendant={shown ? optionId(current) : undefined}
          onChange={(event) => { setText(event.target.value); setActive(0); setOpen(true); }} onKeyDown={onKey}
          onBlur={() => setOpen(false)} onFocus={() => setOpen(true)} />
        {/* Mouse down must not blur the box, or the list would close before the click picks. */}
        <ul id={listId} role="listbox" className="contact-picker-results" hidden={!shown} aria-busy={found.isFetching}
          onMouseDown={(event) => event.preventDefault()}>
          {shown && items.map((contact, index) => (
            <li key={contact.id} id={optionId(index)} role="option" aria-selected={index === current} aria-disabled={disabled || undefined}
              onClick={() => { if (!disabled) onPick(contact); }} onMouseMove={() => { if (index !== current) setActive(index); }}>
              <span className="contact-picker-name">{contact.name}</span>
              <span className="contact-picker-email">{contact.organization ? `${contactAddress(contact)} · ${contact.organization}` : contactAddress(contact)}</span>
            </li>
          ))}
        </ul>
      </div>
      {found.isError && <p role="alert" className="error-copy">{contactMessages.contactsFailed}</p>}
      {settled && items.length === 0 && <p className="muted-copy">{emptyText}</p>}
    </div>
  );
}
