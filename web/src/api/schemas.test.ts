import { describe, expect, it } from 'vitest';
import { contactListSchema } from './schemas';
import { contactAddress } from '../features/contacts/contact-messages';

const known = {
  id: '11111111-1111-4111-8111-111111111111', name: 'Ann', email: 'ann@example.com', organization: null, merged_into_id: null,
  created_at: '2026-10-24T10:00:00Z', updated_at: '2026-10-24T10:00:00Z',
};

describe('contact without email', () => {
  it('parses a list where a customer is known only on a channel', () => {
    const list = contactListSchema.parse({ items: [known, { ...known, id: '22222222-2222-4222-8222-222222222222', name: 'Minh', email: null,
      identities: [{ channel_id: '33333333-3333-4333-8333-333333333333', kind: 'chat_user', value: '4410233', display_name: 'Minh T.' }] }] });
    expect(list.items).toHaveLength(2);
    expect(contactAddress(list.items[1])).toBe('Minh T.');
    expect(contactAddress(list.items[0])).toBe('ann@example.com');
    expect(contactAddress({ email: null, identities: [] })).toBe('No email');
  });
});
