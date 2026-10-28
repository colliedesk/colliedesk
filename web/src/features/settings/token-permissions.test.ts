// @vitest-environment node
import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';
import { allPermissions, presetPermissions } from './token-permissions';

// The contract's TokenPermission enum is the server's list in the server's order (a Go test holds it there).
function contractPermissions(): string[] {
  const yaml = readFileSync(new URL('../../../../contracts/openapi.yaml', import.meta.url), 'utf8');
  const block = yaml.slice(yaml.indexOf('\n    TokenPermission:'));
  const list = block.slice(block.indexOf('      enum:\n') + '      enum:\n'.length);
  const lines = list.split('\n');
  const end = lines.findIndex((line) => !line.startsWith('        - '));
  return lines.slice(0, end).map((line) => line.slice('        - '.length).trim());
}

test('the token form lists every permission of the contract, in its order', () => {
  expect(allPermissions.map((item) => item.name)).toEqual(contractPermissions());
});

test('a tier holds every permission of its own preset and below', () => {
  expect(presetPermissions('read')).toContain('kb_articles:read');
  expect(presetPermissions('read')).not.toContain('messages:note');
  expect(presetPermissions('draft')).toEqual(expect.arrayContaining(['messages:note', 'conversations:update', 'saved_views:delete']));
  expect(presetPermissions('draft')).not.toContain('messages:send');
  expect(presetPermissions('send')).toEqual(expect.arrayContaining(['messages:send', 'kb_articles:publish']));
  expect(presetPermissions('send')).not.toContain('kb_media:purge');
  expect(presetPermissions('admin')).toHaveLength(allPermissions.length);
});
