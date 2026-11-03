// @vitest-environment node
import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';
import { allPermissions, permissionSummary } from './token-permissions';

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

test('every permission says in a sentence what it allows', () => {
  for (const item of allPermissions) expect(item.describe.length).toBeGreaterThan(5);
});

test('a summary counts the permissions and those that reach outside the workspace', () => {
  expect(permissionSummary(['tags:read'])).toBe('1 permission');
  expect(permissionSummary(['messages:read', 'messages:send', 'workspace:export'])).toBe('3 permissions, 2 reach outside the workspace');
});
