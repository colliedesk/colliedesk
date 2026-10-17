import { expect, test } from 'vitest';
import { describeUserAgent } from './describe-user-agent';

test.each([
  ['Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36', 'Chrome on Linux'],
  ['Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15', 'Safari on macOS'],
  ['Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0', 'Edge on Windows'],
  ['Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0', 'Firefox on Windows'],
  ['Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36', 'Chrome on Android'],
  ['Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0 Mobile/15E148 Safari/604.1', 'Chrome on iPhone'],
])('%s reads as %s', (agent, expected) => {
  expect(describeUserAgent(agent)).toBe(expected);
});

test('labels that are not a browser stay as they are', () => {
  expect(describeUserAgent('Cloud sign-in')).toBeNull();
  expect(describeUserAgent('collie-cli/1.4')).toBeNull();
});
