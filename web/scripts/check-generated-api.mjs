import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const expected = join(root, 'src/api/generated.ts');
const temporary = join(mkdtempSync(join(tmpdir(), 'colliedesk-openapi-')), 'generated.ts');
execFileSync(join(root, 'node_modules/.bin/openapi-typescript'), [join(root, '../contracts/openapi.yaml'), '-o', temporary], { stdio: 'ignore' });
if (readFileSync(expected, 'utf8') !== readFileSync(temporary, 'utf8')) {
  console.error('Generated API types are stale. Run: corepack pnpm generate:api');
  process.exit(1);
}
console.log('Generated API types are current.');
