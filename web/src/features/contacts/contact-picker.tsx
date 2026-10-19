import { useEffect, useId, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { contactApi } from './contact-api';
import type { Contact } from '../../api/schemas';
import { messages } from '../../messages';
import { contactMessages } from './contact-messages';

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

// ContactPicker finds a contact by name or email. Merged contacts are never offered: the server leaves
// them out of a search.
export function ContactPicker({ label, onPick, excludeId, autoFocus, disabled, emptyText = contactMessages.mergeSearchEmpty }: ContactPickerProps) {
  const [text, setText] = useState('');
  const [term, setTerm] = useState('');
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
  return (
    <div className="contact-picker">
      <label htmlFor={inputId}>{label}</label>
      <input id={inputId} type="search" value={text} autoFocus={autoFocus} disabled={disabled} autoComplete="off"
        aria-controls={listId} onChange={(event) => setText(event.target.value)} />
      <ul id={listId} className="contact-picker-results" aria-busy={found.isFetching}>
        {term !== '' && items.map((contact) => (
          <li key={contact.id}>
            <button type="button" disabled={disabled} onClick={() => onPick(contact)}>
              <span className="contact-picker-name">{contact.name}</span>
              <span className="contact-picker-email">{contact.email}</span>
              {contact.organization && <span className="contact-picker-email">{contact.organization}</span>}
            </button>
          </li>
        ))}
      </ul>
      {found.isError && <p role="alert" className="error-copy">{contactMessages.contactsFailed}</p>}
      {settled && items.length === 0 && <p className="muted-copy">{emptyText}</p>}
    </div>
  );
}
