import { useState } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { api } from '../../api/client';
import { contactApi } from './contact-api';
import type { Contact, ContactMerge } from '../../api/schemas';
import { ContactPage } from './contact-page';

// A page of history fits a screen; the count on the page says how many there are in all.
export const contactHistoryPageSize = 25;

export function ContactRoute() {
  const { contactId } = useParams({ strict: false }) as { contactId: string };
  const client = useQueryClient();
  const navigate = useNavigate();
  const [paged, setPaged] = useState({ contactId, page: 1 });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const contact = useQuery({ queryKey: ['contact', contactId], queryFn: () => contactApi.contact(contactId) });
  // Another contact's page starts on its first page, and a page past the last one (after an undo, or a
  // merge elsewhere) shows the last page instead of an empty list.
  const pages = Math.max(1, Math.ceil((contact.data?.conversation_count ?? 0) / contactHistoryPageSize));
  const asked = paged.contactId === contactId ? paged.page : 1;
  const page = contact.data ? Math.min(asked, pages) : asked;
  // The history goes out with the contact, not after it: both need only the id.
  const query = new URLSearchParams({ contact: contactId, page: String(page), page_size: String(contactHistoryPageSize) }).toString();
  const history = useQuery({ queryKey: ['conversations', query], queryFn: () => api.conversations(query), placeholderData: keepPreviousData });
  // A rename, merge or undo changes this page, the history of the other contact and the names in the inbox.
  const changed = async () => {
    await Promise.all([client.invalidateQueries({ queryKey: ['contact'] }), client.invalidateQueries({ queryKey: ['conversations'] }),
      client.invalidateQueries({ queryKey: ['contacts'] })]);
  };
  return <ContactPage contact={contact.data} conversations={history.data?.items} page={page} pageSize={contactHistoryPageSize}
    onPage={(next) => setPaged({ contactId, page: next })} timezone={me.data?.timezone}
    loading={contact.isPending} failed={contact.isError} historyLoading={history.isPending}
    onRename={async (name: string) => { await contactApi.renameContact(contactId, name); await changed(); }}
    onMerge={async (duplicate: Contact) => { await contactApi.mergeContact(contactId, duplicate.id); await changed(); }}
    onUndoMerge={async (merge: ContactMerge) => { await contactApi.undoMerge(contactId, merge.id); await changed(); }}
    onOpenConversation={(id) => void navigate({ to: '/conversations/$conversationId', params: { conversationId: id } })}
    onOpenContact={(id) => void navigate({ to: '/contacts/$contactId', params: { contactId: id } })} />;
}
