import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, test } from 'vitest';

// A person using a workspace cannot act on a server variable such as GOOGLE_OAUTH_CLIENT_ID: screens say
// that a feature is not set up on this server and to ask whoever runs it. The variable names belong in
// docs/self-host. This reads every string the web app could put on screen.
const root = join(__dirname);
const skipped = /(\.test\.tsx?|generated\.ts|schemas\.ts|\.d\.ts)$/;
const variable = /\b[A-Z][A-Z0-9]*_[A-Z0-9_]*[A-Z0-9]\b/;
// A string literal ('…', "…" or `…`) on one line, outside comments.
const literal = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|`(?:[^`\\]|\\.)*`/g;
// Values that are not server variables a person is asked to set. ANTHROPIC_API_KEY is the secret Claude's
// GitHub workflow reads in the team's own repository, which Autofix's checklist asks the admin to add there.
const allowed = new Set(['YOUR_INBOUND_SECRET', 'ANTHROPIC_API_KEY']);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !skipped.test(name) ? [path] : [];
  });
}

test('no screen shows a server variable name', () => {
  const found: string[] = [];
  for (const path of sources(root)) {
    const code = readFileSync(path, 'utf8').split('\n').filter((line) => !/^\s*(\/\/|\*|\/\*)/.test(line)).join('\n')
      .replace(/import\.meta\.env\.\w+/g, '');
    for (const match of code.match(literal) ?? []) {
      const name = match.replace(/\$\{[^}]*\}/g, '').match(variable)?.[0];
      if (name && !allowed.has(name)) found.push(`${relative(root, path)}: ${name}`);
    }
  }
  expect(found).toEqual([]);
});
