import { expect, test } from 'vitest';
import { ConflictError } from '../../api/client';
import { OutboundRefusedError } from '../../api/outbound-error';
import { replyRefusalText, retryRefusalText } from './refusal-text';

test('a refused retry says the server\'s reason, not "Try again", when retrying cannot help', () => {
  const reason = 'Gmail sending is not set up on this server: ask whoever runs it.';
  expect(retryRefusalText(new ConflictError(reason))).toBe(reason);
  expect(retryRefusalText(new Error('network'))).toBe('The delivery could not be retried. Try again.');
});

test('a customer who wrote, but whose mail the trial cannot count, is told the real reason, not "wrote to you first"', () => {
  const server = 'This customer\'s email came in through your inbound URL, so Colliedesk cannot confirm who sent it. During the trial, replies go only to mail that arrives at your forwarding address (Settings → Channels → Email). To reply to this customer now, verify your sending domain in Settings or choose a plan.';
  const refusal = new OutboundRefusedError('trial_outbound_restricted', server, undefined, [], undefined, 'mail_not_via_hosted_address');
  expect(replyRefusalText(refusal)).toBe(`${server} Your text is still here.`);
  expect(retryRefusalText(refusal)).toBe(server);
  expect(replyRefusalText(refusal)).not.toMatch(/wrote to you first/);
});
