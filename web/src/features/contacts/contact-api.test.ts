import { afterEach, expect, test, vi } from 'vitest';
import { OutboundRefusedError } from '../../api/outbound-error';
import { contactApi, existingContact } from './contact-api';

const reply = (status: number, body: unknown = {}) => new Response(JSON.stringify(body), { status });
const apiError = (code: string, message = 'Server words.') => ({ error: { code, message } });
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => fetchMock.mockReset());

const conversationBody = {
  id: '33333333-3333-4333-8333-333333333333', contact_id: '44444444-4444-4444-8444-444444444444', contact_name: 'Pat', subject: 'Hello',
  status: 'open', priority: 'normal', assignee_id: null, tags: [], origin: null, created_at: '2026-10-19T00:00:00Z', updated_at: '2026-10-19T00:00:00Z',
};

test('a new conversation with a first reply sends it with the conversation, as files when there are some', async () => {
  fetchMock.mockResolvedValue(reply(201, conversationBody));
  await contactApi.startConversation(conversationBody.contact_id, 'Hello', { body: 'First words', files: [], cc: ['boss@example.com'] });
  let [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe('/api/v1/conversations');
  expect(JSON.parse(init.body as string)).toEqual({ contact_id: conversationBody.contact_id, subject: 'Hello', body: 'First words', cc: ['boss@example.com'] });

  fetchMock.mockResolvedValue(reply(201, conversationBody));
  await contactApi.startConversation(conversationBody.contact_id, 'Hello', { body: 'With a file', files: [new File(['a'], 'a.txt')], cc: ['boss@example.com'] });
  [, init] = fetchMock.mock.calls[1] as [string, RequestInit];
  const form = init.body as FormData;
  expect([...form.keys()]).toEqual(['contact_id', 'subject', 'body', 'cc', 'files']);
  expect(form.get('body')).toBe('With a file');

  fetchMock.mockResolvedValue(reply(201, conversationBody));
  await contactApi.startConversation({ name: 'Pat', email: 'pat@example.com' }, 'New customer', { body: 'Hi', files: [], cc: [] });
  [, init] = fetchMock.mock.calls[2] as [string, RequestInit];
  expect(JSON.parse(init.body as string)).toEqual({ contact: { name: 'Pat', email: 'pat@example.com' }, subject: 'New customer', body: 'Hi' });
});

test('a refused new conversation on the trial is an outbound refusal', async () => {
  fetchMock.mockResolvedValue(reply(403, apiError('trial_outbound_restricted', 'Verify your domain.')));
  await expect(contactApi.startConversation(conversationBody.contact_id, 'Hello', { body: 'Hi', files: [], cc: [] })).rejects.toBeInstanceOf(OutboundRefusedError);
});


test('a 409 contact_exists carries the customer to use', async () => {
  const pat = { id: '44444444-4444-4444-8444-444444444444', name: 'Pat', email: 'pat@example.com', organization: null, merged_into_id: null,
    created_at: '2026-10-19T00:00:00Z', updated_at: '2026-10-19T00:00:00Z' };
  fetchMock.mockResolvedValue(reply(409, { error: { code: 'contact_exists', message: 'Pat is already a customer.', contact: pat } }));
  const error = await contactApi.startConversation({ name: 'Pat', email: 'pat@example.com' }, 'Hi', { body: 'Hi', files: [], cc: [] }).catch((caught: unknown) => caught);
  expect(existingContact(error)).toEqual(pat);
  fetchMock.mockResolvedValue(reply(409, { error: { code: 'contact_merged', message: 'Merged.' } }));
  expect(existingContact(await contactApi.startConversation('x', 'Hi', { body: 'Hi', files: [], cc: [] }).catch((caught: unknown) => caught))).toBeUndefined();
});
