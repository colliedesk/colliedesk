import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import worker from './worker.js';

const realFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = realFetch; });

const env = { INBOUND_URL: 'https://desk.example.test/api/v1/channels/email/inbound', INBOUND_SECRET: 'server-secret-placeholder' };

function message(to, text = 'From: a@customer.example\r\nSubject: s\r\n\r\nbody\r\n') {
  const bytes = new TextEncoder().encode(text);
  const calls = { rejected: [], forwarded: [] };
  return {
    calls,
    to,
    raw: new Response(bytes).body,
    rawSize: bytes.length,
    setReject: (reason) => calls.rejected.push(reason),
    forward: async (address) => { calls.forwarded.push(address); },
  };
}

function stubFetch(...statuses) {
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init, body: new TextDecoder().decode(init.body) });
    const status = statuses.shift() ?? 200;
    if (status === 'network') throw new TypeError('network down');
    return new Response('{}', { status });
  };
  return requests;
}

test('posts the raw message with the envelope recipient on top and the server secret', async () => {
  const requests = stubFetch(200);
  const mail = message('support@acme.colliedesk.com');
  await worker.email(mail, env);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, env.INBOUND_URL);
  assert.equal(requests[0].init.headers['X-CollieDesk-Inbound-Secret'], env.INBOUND_SECRET);
  assert.equal(requests[0].init.headers['Content-Type'], 'message/rfc822');
  assert.ok(requests[0].body.startsWith('X-Original-To: support@acme.colliedesk.com\r\nFrom: a@customer.example'));
  assert.deepEqual(mail.calls, { rejected: [], forwarded: [] });
});

test('rejects permanently when the server says the address or message is unacceptable', async () => {
  for (const status of [400, 404, 413]) {
    const requests = stubFetch(status);
    const mail = message('support@nobody.colliedesk.com');
    await worker.email(mail, { ...env, FALLBACK_ADDRESS: 'owner@example.test' });
    assert.equal(requests.length, 1, `status ${status} must not be retried`);
    assert.equal(mail.calls.rejected.length, 1, `status ${status} must reject`);
    assert.deepEqual(mail.calls.forwarded, []);
  }
});

test('retries a temporary failure, then succeeds without rejecting', async () => {
  const requests = stubFetch(503, 'network', 200);
  const mail = message('support@acme.colliedesk.com');
  await worker.email(mail, { ...env, RETRY_DELAY_MS: '0' });
  assert.equal(requests.length, 3);
  assert.deepEqual(mail.calls, { rejected: [], forwarded: [] });
});

test('after repeated temporary failures it forwards to the fallback address, never drops', async () => {
  for (const status of [429, 500, 401]) {
    stubFetch(status, status, status);
    const mail = message('support@acme.colliedesk.com');
    await worker.email(mail, { ...env, FALLBACK_ADDRESS: 'owner@example.test', RETRY_DELAY_MS: '0' });
    assert.deepEqual(mail.calls.forwarded, ['owner@example.test'], `status ${status}`);
    assert.deepEqual(mail.calls.rejected, []);
  }
});

test('without a fallback address a lasting failure rejects so the sender is told', async () => {
  stubFetch(500, 500, 500);
  const mail = message('support@acme.colliedesk.com');
  await worker.email(mail, { ...env, RETRY_DELAY_MS: '0' });
  assert.equal(mail.calls.rejected.length, 1);
});

test('refuses to run without its configuration instead of posting without a secret', async () => {
  const requests = stubFetch(200);
  const mail = message('support@acme.colliedesk.com');
  await worker.email(mail, { INBOUND_URL: env.INBOUND_URL });
  assert.equal(requests.length, 0);
  assert.equal(mail.calls.rejected.length, 1);
});
