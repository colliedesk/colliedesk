import { expect, test } from 'vitest';
import { ConflictError } from '../../api/client';
import { retryRefusalText } from './refusal-text';

test('a refused retry says the server\'s reason, not "Try again", when retrying cannot help', () => {
  const reason = 'Gmail sending is not configured on this server. Ask the administrator to set GOOGLE_OAUTH_CLIENT_ID.';
  expect(retryRefusalText(new ConflictError(reason))).toBe(reason);
  expect(retryRefusalText(new Error('network'))).toBe('The delivery could not be retried. Try again.');
});
