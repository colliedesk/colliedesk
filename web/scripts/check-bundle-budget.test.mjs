import { randomBytes } from 'node:crypto';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { afterEach, expect, test } from 'vitest';
import { checkBundleBudget } from './check-bundle-budget.mjs';

const directories = [];

afterEach(async () => {
  await Promise.all(directories.splice(0).map((directory) => rm(directory, { recursive: true })));
});

async function assets() {
  const directory = await mkdtemp(path.join(tmpdir(), 'colliedesk-bundle-'));
  directories.push(directory);
  const assetDirectory = path.join(directory, 'assets');
  await mkdir(assetDirectory);
  return assetDirectory;
}

test('accepts JS and CSS assets within gzip budgets', async () => {
  const directory = await assets();
  await writeFile(path.join(directory, 'index.js'), 'const ok = true;'.repeat(100));
  await writeFile(path.join(directory, 'index.css'), '.ok{display:block}'.repeat(100));

  await expect(checkBundleBudget(path.dirname(directory))).resolves.toBeUndefined();
});

test('rejects an oversized main JS asset', async () => {
  const directory = await assets();
  await writeFile(path.join(directory, 'index.js'), randomBytes(220 * 1024));
  await writeFile(path.join(directory, 'index.css'), '.ok{}');

  await expect(checkBundleBudget(path.dirname(directory))).rejects.toThrow('JS gzip budget exceeded');
});

test('rejects an oversized main CSS asset', async () => {
  const directory = await assets();
  await writeFile(path.join(directory, 'index.js'), 'const ok = true;');
  await writeFile(path.join(directory, 'index.css'), randomBytes(40 * 1024));

  await expect(checkBundleBudget(path.dirname(directory))).rejects.toThrow('CSS gzip budget exceeded');
});
