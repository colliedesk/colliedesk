import { useQuery } from '@tanstack/react-query';
import { contactApi } from './contact-api';
import { contactAddress, contactMessages } from './contact-messages';

// How many customers a search shows above the conversations; more is what the picker is for.
const matchLimit = 5;

// ContactMatches lists the customers whose name or email matches the inbox search, so a search for a
// person also reaches their page. It is its own chunk; the inbox shows it once that is loaded.
export function ContactMatches({ search }: { search: string }) {
  const term = search.trim();
  const found = useQuery({ queryKey: ['contacts', 'search', term, 'inbox'], queryFn: ({ signal }) => contactApi.searchContacts(term, matchLimit, signal), enabled: term !== '' });
  const items = term ? found.data?.items ?? [] : [];
  if (items.length === 0) return null;
  return (
    <nav className="contact-matches" aria-label={contactMessages.contactMatches}>
      <h2>{contactMessages.contactMatches}</h2>
      <ul>{items.map((contact) => <li key={contact.id}>
        <a href={`/contacts/${contact.id}`}>
          <span className="contact-picker-name">{contact.name}</span> <span className="contact-picker-email">{contactAddress(contact)}</span></a>
      </li>)}</ul>
    </nav>
  );
}
