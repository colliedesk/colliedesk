import { expect, test } from 'vitest';
import { channelNeedsAction } from './channel-problem';

const channel = (state: string, reason: string | null, lastInbound: string | null) => ({ status: { state, reason_code: reason }, last_inbound_at: lastInbound });
const seen = '2026-11-10T08:00:00Z';

// The menu's dot says a channel that was working has a problem the admin must act on. A channel still being set up
// (nothing has arrived, the setup is unfinished) is not a problem: Quick start and the channel's own page lead that.
test('a channel still in setup is not a problem, whatever the server says of it', () => {
  expect(channelNeedsAction(channel('needs_attention', 'no_origins', null))).toBe(false);
  expect(channelNeedsAction(channel('needs_attention', 'ai_chat_off', null))).toBe(false);
  expect(channelNeedsAction(channel('needs_attention', 'secret_missing', null))).toBe(false);
  expect(channelNeedsAction(channel('needs_attention', 'token_missing', null))).toBe(false);
  expect(channelNeedsAction(channel('needs_attention', 'domain_unverified', null))).toBe(false);
  expect(channelNeedsAction(channel('needs_attention', 'no_inbound_secret', null))).toBe(false);
});

test('a channel that has received something and now needs attention is a problem', () => {
  for (const reason of ['no_origins', 'ai_chat_off', 'domain_unverified', 'cannot_send', null]) expect(channelNeedsAction(channel('needs_attention', reason, seen)), String(reason)).toBe(true);
});

test('a lost connection is a problem even before any message arrived', () => {
  for (const reason of ['gmail_broken', 'microsoft_broken', 'signature_failed', 'sending_paused']) expect(channelNeedsAction(channel('needs_attention', reason, null)), reason).toBe(true);
});

test('working, waiting, off, not connected and removed are never a problem', () => {
  for (const state of ['working', 'waiting', 'off', 'not_connected', 'removed']) expect(channelNeedsAction(channel(state, null, seen)), state).toBe(false);
});
