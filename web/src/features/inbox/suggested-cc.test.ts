import { expect, test } from 'vitest';
import type { Message } from '../../api/schemas';
import { suggestedCc } from './suggested-cc';

const base = { conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null };
let counter = 0;
type Via = 'platform' | 'workspace' | 'web_form';
const incoming = (from: string, cc?: string[], authentication: 'pass' | 'fail' | 'none' = 'pass', via: Via = 'platform'): Message => ({
  ...base, id: `00000000-0000-4000-8000-${String(++counter).padStart(12, '0')}`, kind: 'incoming', body: 'Hi',
  email: { from, authentication, via, attachments: [], original_attachment_id: null, ...(cc ? { cc } : {}) },
});
const outgoing = (cc: string[]): Message => ({ ...base, id: `00000000-0000-4000-8000-${String(++counter).padStart(12, '0')}`, kind: 'outgoing', body: 'Re', email: null, cc });

test('offers what the customer copied on their latest message', () => {
  expect(suggestedCc([incoming('ada@customer.example', ['boss@customer.example', 'ops@customer.example'])]))
    .toEqual(['boss@customer.example', 'ops@customer.example']);
});

test('uses the latest incoming message, not an older one and not our own replies', () => {
  const timeline = [incoming('ada@customer.example', ['old@customer.example']), outgoing(['sent@customer.example']), incoming('ada@customer.example', ['new@customer.example']), outgoing([])];
  expect(suggestedCc(timeline)).toEqual(['new@customer.example']);
});

test('never offers the customer who opened the conversation, whatever the case', () => {
  const timeline = [incoming('Ada@Customer.example', []), incoming('boss@customer.example', ['ADA@customer.example', 'ops@customer.example'])];
  expect(suggestedCc(timeline)).toEqual(['boss@customer.example', 'ops@customer.example']);
});

test('a copied person who answered with reply-all is offered too', () => {
  expect(suggestedCc([incoming('ada@customer.example'), incoming('boss@customer.example', ['ada@customer.example'])])).toEqual(['boss@customer.example']);
});

test('mail kept before Cc was recorded, notes and web form text offer nothing', () => {
  expect(suggestedCc([incoming('ada@customer.example')])).toEqual([]);
  expect(suggestedCc([])).toEqual([]);
  expect(suggestedCc([outgoing(['x@customer.example'])])).toEqual([]);
});

test('offers at most ten addresses', () => {
  const many = Array.from({ length: 14 }, (_, index) => `p${index}@customer.example`);
  expect(suggestedCc([incoming('ada@customer.example', many)])).toEqual(many.slice(0, 10));
});

test('mail whose sender was not verified offers nothing: anyone can write its Cc line', () => {
  expect(suggestedCc([incoming('ada@customer.example', ['boss@customer.example'], 'none')])).toEqual([]);
  expect(suggestedCc([incoming('ada@customer.example', ['boss@customer.example'], 'fail')])).toEqual([]);
  const latestUnverified = [incoming('ada@customer.example', ['boss@customer.example']), incoming('mallory@elsewhere.example', ['victim@elsewhere.example'], 'none')];
  expect(suggestedCc(latestUnverified)).toEqual([]);
});

test('on the hosted cloud, mail that did not come through the platform forwarder offers nothing: the server would refuse it', () => {
  const posted = [incoming('ada@customer.example', ['boss@customer.example'], 'pass', 'workspace')];
  expect(suggestedCc(posted, { hosted: true })).toEqual([]);
  expect(suggestedCc([incoming('ada@customer.example', ['boss@customer.example'], 'pass', 'web_form')], { hosted: true })).toEqual([]);
  expect(suggestedCc([incoming('ada@customer.example', ['boss@customer.example'])], { hosted: true })).toEqual(['boss@customer.example']);
  // A self-hosted desk receives all its mail on its own inbound address.
  expect(suggestedCc(posted)).toEqual(['boss@customer.example']);
});
