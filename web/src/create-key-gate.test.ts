import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, test } from 'vitest';

// The web side of TestEveryCreateRouteTakesAnIdempotencyKey (server/internal/httpapi): a create the
// contract keys is sent with its Idempotency-Key, and that key comes from api/use-create.ts, whose hooks also
// disable the press while it is in flight. The API functions of those creates take the key as a required
// argument, so a call outside the hooks has no key to give unless it makes one, which only these files may.
const root = join(__dirname);
const keyMakers: Record<string, string> = {
  'api/use-create.ts': 'The shared create hooks: one request per press, under the key of its input.',
  'features/inbox/reply-composer.tsx': 'A reply\'s key is stored with its draft, so a page reloaded mid-send asks the server about it.',
};

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/(\.test\.tsx?|\.d\.ts)$/.test(name) ? [path] : [];
  });
}

const files = sources(root).map((path) => ({ name: relative(root, path), text: readFileSync(path, 'utf8') }));

test('only the create hooks and the reply box make an Idempotency-Key', () => {
  const makers = files.filter((file) => file.name !== 'api/submit-key.ts' && /from '[./]*(api\/)?submit-key'/.test(file.text)).map((file) => file.name);
  expect(makers.length).toBeGreaterThan(0);
  expect(makers.sort()).toEqual(Object.keys(keyMakers).sort());
});

// The POST paths the contract gives an Idempotency-Key, read from the generated types.
function keyedPaths(): string[] {
  const generated = readFileSync(join(root, 'api/generated.ts'), 'utf8');
  const keyed = new Set<string>();
  for (const match of generated.matchAll(/^ {4}(\w+): \{\n(?:(?!\n {4}\w+: \{\n)[\s\S])*?"Idempotency-Key"\?:/gm)) keyed.add(match[1]!);
  const paths: string[] = [];
  for (const match of generated.matchAll(/^ {4}"(\/api\/v1\/[^"]+)": \{[\s\S]*?^ {8}post(\?)?: (?:operations\["(\w+)"\]|never);/gm)) {
    if (match[3] && keyed.has(match[3])) paths.push(match[1]!);
  }
  return paths;
}

test('every create the contract keys is sent with an Idempotency-Key', () => {
  const paths = keyedPaths();
  expect(paths).toEqual(expect.arrayContaining(['/api/v1/auth/invites', '/api/v1/tags', '/api/v1/channels', '/api/v1/conversations/{id}/messages']));
  // A path parameter ({id}) is written as a template value (${id}) in the web app.
  const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
  const pattern = (path: string) => new RegExp(`['\`]${path.split(/\{[^}]+\}/).map(escape).join('\\$\\{[^}]+\\}')}['\`?]`);
  for (const path of paths) {
    const sent = files.flatMap((file) => {
      const lines = file.text.split('\n');
      return lines.flatMap((line, index) => {
        if (!pattern(path).test(line)) return [];
        const call = lines.slice(index, index + 3).join('\n');
        return /method: 'POST'/.test(call) ? [{ where: `${file.name}:${index + 1}`, keyed: /'Idempotency-Key'/.test(call) }] : [];
      });
    });
    for (const call of sent) expect(call, `${call.where} posts ${path} without its Idempotency-Key`).toMatchObject({ keyed: true });
  }
});
