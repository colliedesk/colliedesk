import { expect, test } from 'vitest';
import { localPath } from './workspace-host';

test('localPath keeps a path of this site', () => {
  expect(localPath('/settings?tab=x')).toBe('/settings?tab=x');
  expect(localPath('/')).toBe('/');
});

// A browser drops tabs and line breaks from an address, so "/\t/evil.com" opens //evil.com.
test('localPath refuses anything a browser reads as another host', () => {
  for (const path of ['//evil.com', '/\\evil.com', 'https://evil.com', '/\t/evil.com', '/\n/evil.com', '/\x00x', '/a\x7f', '/\x1f/evil.com', '', undefined]) {
    expect(localPath(path)).toBe('/');
  }
});
