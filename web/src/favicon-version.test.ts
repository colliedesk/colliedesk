// @vitest-environment node
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';

const webDir = new URL('../', import.meta.url);

// Browsers keep a favicon per URL and ignore Cache-Control, so a changed icon is only seen when its
// URL changes. Each icon link in index.html carries ?v=<first 10 hex of the file's sha256>.
test('every icon link in index.html is versioned by the hash of its file', () => {
  const html = readFileSync(new URL('index.html', webDir), 'utf8');
  const hrefs = [...html.matchAll(/<link rel="(?:icon|apple-touch-icon)"[^>]*href="([^"]+)"/g)].map((m) => m[1]);
  expect(hrefs).toHaveLength(3);
  for (const href of hrefs) {
    const [path, version] = href.split('?v=');
    const hash = createHash('sha256').update(readFileSync(new URL(`public${path}`, webDir))).digest('hex').slice(0, 10);
    expect(href).toBe(`${path}?v=${hash}`);
  }
});
