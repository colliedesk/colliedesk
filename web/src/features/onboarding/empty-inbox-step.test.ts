import { expect, test } from 'vitest';
import type { SetupProgress } from '../../api/schemas';
import { permissionsOf } from '../../test-roles';
import { emptyInboxStep } from './setup-flow-path';

// The six steps in the order the server gives them.
const order = ['email', 'import', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'];
function progress(states: Record<string, string> = {}): SetupProgress {
  const all = { email: 'none', import: 'none', ai_drafts: 'none', reply_targets: 'none', invite: 'alone', first_reply: 'none', ...states } as Record<string, string>;
  return { steps: order.map((key) => ({ key, name: key, state: all[key], detail: null })), complete: false, recommended: null } as unknown as SetupProgress;
}

test('a workspace with nothing set up is offered the email step first', () => {
  expect(emptyInboxStep(progress(), { permissions: permissionsOf('admin') })).toBe('email');
});

test('an address that waits for its first email is done on the person\'s side: the import is offered', () => {
  expect(emptyInboxStep(progress({ email: 'waiting' }), { permissions: permissionsOf('admin') })).toBe('import');
});

test('with email working, the import is the next way to fill an empty inbox', () => {
  expect(emptyInboxStep(progress({ email: 'working' }), { permissions: permissionsOf('admin') })).toBe('import');
});

test('with email working and the history imported, there is nothing to offer: an empty inbox has no first email to answer yet', () => {
  expect(emptyInboxStep(progress({ email: 'working', import: 'done' }), { permissions: permissionsOf('admin') })).toBeUndefined();
});

test('a person whose roles cannot manage channels is offered nothing', () => {
  expect(emptyInboxStep(progress(), { permissions: permissionsOf('agent') })).toBeUndefined();
});

test('before the steps are known nothing is offered', () => {
  expect(emptyInboxStep(undefined, { permissions: permissionsOf('admin') })).toBeUndefined();
});
