import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, test } from 'vitest';

// A link to a page of the app is a plain <a href>; useInAppLinks (lib/in-app-links.ts) follows it through the
// router. A second way to follow a link (a link component, a click callback that only navigates) is a second
// convention to keep in step, so none may come back.
const root = join(__dirname, '..');
const banned = [
  { pattern: /\bRouterLink\b/, why: 'a link component that follows itself' },
  { pattern: /\bfunction AppLink\b/, why: 'a link component that follows itself' },
  { pattern: /\bonOpenSettings\b/, why: 'a callback that only navigates to Settings' },
  { pattern: /\bonOpenContact\b/, why: 'a callback that only navigates to a contact' },
];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.test\.tsx$/.test(name) ? [path] : [];
  });
}

test('a link inside the app is a plain <a href> that useInAppLinks follows', () => {
  const found = sources(root).flatMap((path) => {
    const lines = readFileSync(path, 'utf8').split('\n');
    return lines.flatMap((line, index) => banned.filter((rule) => rule.pattern.test(line))
      .map((rule) => `${relative(root, path)}:${index + 1} has ${rule.why}`));
  });
  expect(found, 'write a plain <a href="/page">; useInAppLinks opens it without a reload').toEqual([]);
});
