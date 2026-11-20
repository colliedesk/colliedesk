import { expect, test } from 'vitest';
import { savedReplyChangeText } from './saved-reply-change';

test('a save that kept the changes of a published article off the help center says so, with who saved them', () => {
  expect(savedReplyChangeText({ kind: 'draft', restored_from_version: null, changed_by: { agent_id: null, name: 'Ada' }, token: null, agent_request_id: null }))
    .toBe('Saved unpublished changes by Ada');
});
