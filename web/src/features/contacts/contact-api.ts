import { z } from 'zod';
import { ConflictError, json, request } from '../../api/client';
import { contactListSchema, contactSchema, conversationSchema } from '../../api/schemas';
import type { Contact } from '../../api/schemas';
import type { components } from '../../api/generated';

type Generated<Name extends keyof components['schemas']> = components['schemas'][Name];
const nullableDate = z.string().datetime().nullable();

// The schemas of the customer page live with it, out of the inbox bundle.
export const contactMergeSchema = z.object({
  id: z.string().uuid(), contact_id: z.string().uuid(), merged_contact: contactSchema, conversation_ids: z.array(z.string().uuid()),
  merged_by_agent_id: z.string().uuid().nullable(), merged_at: z.string().datetime(), undone_at: nullableDate,
}) satisfies z.ZodType<Generated<'ContactMerge'>>;
export const contactDetailSchema = contactSchema.extend({
  conversation_count: z.number().int().nonnegative(), merged_into: contactSchema.nullable(), merges: z.array(contactMergeSchema),
}) satisfies z.ZodType<Generated<'ContactDetail'>>;

// The calls only the customer page and the window that starts a conversation make. They live in those
// chunks, not in the inbox bundle.

// The first reply of a new conversation, sent with the conversation itself.
export interface FirstReply {
  body: string;
  files: File[];
  cc: string[];
}

// Who a new conversation is with: a contact's id, or a customer added with it in the same transaction.
export type ConversationContact = string | { name: string; email: string };

function conversationForm(contact: ConversationContact, subject: string, reply: FirstReply): FormData {
  const form = new FormData();
  if (typeof contact === 'string') form.append('contact_id', contact);
  else { form.append('contact_name', contact.name); form.append('contact_email', contact.email); }
  form.append('subject', subject);
  form.append('body', reply.body);
  // The server reads the copied addresses before the files, so a refusal costs no upload.
  for (const address of reply.cc) form.append('cc', address);
  for (const file of reply.files) form.append('files', file, file.name);
  return form;
}

export const contactApi = {
  // A short list for a search as you type; signal cancels a search the person typed past.
  searchContacts: (search: string, limit: number, signal?: AbortSignal) =>
    request(`/api/v1/contacts?${new URLSearchParams({ search, limit: String(limit) })}`, contactListSchema, { signal }),
  contact: (id: string) => request(`/api/v1/contacts/${id}`, contactDetailSchema),
  // Starts the conversation by emailing the contact; a refused email stores nothing, not even a new customer.
  startConversation: (contact: ConversationContact, subject: string, reply: FirstReply) => {
    const who = typeof contact === 'string' ? { contact_id: contact } : { contact };
    const body = reply.files.length > 0 ? conversationForm(contact, subject, reply)
      : json<'createConversation'>(reply.cc.length > 0 ? { ...who, subject, body: reply.body, cc: reply.cc } : { ...who, subject, body: reply.body });
    return request('/api/v1/conversations', conversationSchema, { method: 'POST', body });
  },
  renameContact: (id: string, name: string) => request(`/api/v1/contacts/${id}`, contactSchema, { method: 'PATCH', body: json<'updateContact'>({ name }) }),
  // Merges the duplicate into the contact kept (id); undoMerge moves its conversations back.
  mergeContact: (id: string, duplicateId: string) => request(`/api/v1/contacts/${id}/merges`, contactMergeSchema, { method: 'POST', body: json<'mergeContact'>({ contact_id: duplicateId }) }),
  undoMerge: (id: string, mergeId: string) => request(`/api/v1/contacts/${id}/merges/${mergeId}`, contactMergeSchema, { method: 'DELETE' }),
};

// existingContact reads the 409 `contact_exists` answer: the address is already a customer, and this is
// the one to use instead (the contact it was merged into, when it was merged).
export function existingContact(error: unknown): Contact | undefined {
  if (!(error instanceof ConflictError) || error.detail?.code !== 'contact_exists') return undefined;
  const parsed = contactSchema.safeParse(error.detail?.contact);
  return parsed.success ? parsed.data : undefined;
}
