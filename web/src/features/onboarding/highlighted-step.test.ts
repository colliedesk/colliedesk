import { expect, test } from 'vitest';
import type { SetupProgress } from '../../api/schemas';
import { permissionsOf } from '../../test-roles';
import { highlightedStep } from './setup-steps';

// The six steps in the order the server gives them.
const order = ['email', 'import', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'];
function progress(states: Record<string, string> = {}): SetupProgress {
  const all = { email: 'none', import: 'none', ai_drafts: 'none', reply_targets: 'none', invite: 'alone', first_reply: 'none', ...states } as Record<string, string>;
  return { steps: order.map((key) => ({ key, name: key, state: all[key], detail: null })), complete: false, recommended: null } as unknown as SetupProgress;
}
const admin = { permissions: permissionsOf('admin') };
const agent = { permissions: permissionsOf('agent') };

test('a workspace with nothing set up highlights the email step', () => {
  expect(highlightedStep(progress(), admin, false)).toBe('email');
});

test('an address that waits for its first email is still the highlighted step, as in Quick start', () => {
  expect(highlightedStep(progress({ email: 'waiting' }), admin, false)).toBe('email');
});

test('with email working, the import is next; with the history imported too, answering the first email is', () => {
  expect(highlightedStep(progress({ email: 'working' }), admin, false)).toBe('import');
  expect(highlightedStep(progress({ email: 'working', import: 'done' }), admin, false)).toBe('first_reply');
});

test('once Set up is done, the Try more steps follow, unless the person skipped them', () => {
  const setUpDone = { email: 'working', import: 'done', first_reply: 'sent' };
  expect(highlightedStep(progress(setUpDone), admin, false)).toBe('ai_drafts');
  expect(highlightedStep(progress(setUpDone), admin, true)).toBeUndefined();
});

test('a step with nothing to do on this server is passed over', () => {
  expect(highlightedStep(progress({ email: 'working', import: 'done', first_reply: 'sent', ai_drafts: 'unavailable' }), admin, false)).toBe('reply_targets');
});

test('a person whose roles cannot manage channels is highlighted the first step they may take', () => {
  expect(highlightedStep(progress(), agent, false)).toBe('first_reply');
  expect(highlightedStep(progress({ first_reply: 'sent' }), agent, false)).toBeUndefined();
});

test('before the steps are known nothing is highlighted', () => {
  expect(highlightedStep(undefined, admin, false)).toBeUndefined();
});
