// Keeps the "Onboarding journeys" list in AGENTS.md and the @onboarding end-to-end tests in
// web/e2e identical, so a journey cannot be deleted, renamed, untagged, skipped or narrowed by .only (and so dropped from the
// onboarding CI job) without the list, and the people reading it, noticing.
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const webDirectory = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function listedJourneys(agentsMarkdown) {
  const section = agentsMarkdown.split(/^## /m).find((part) => part.startsWith('Onboarding journeys')) ?? '';
  return [...section.matchAll(/^- `([^`]+)`/gm)].map((match) => match[1]);
}

function taggedJourneys(specSources) {
  const pattern = /\btest\('((?:[^'\\]|\\.)*)',\s*\{\s*tag:\s*'@onboarding'/g;
  return Object.values(specSources).flatMap((source) => [...source.matchAll(pattern)].map((match) => match[1].replace(/\\(.)/g, '$1')));
}

// A journey that is skipped, marked fixme/fail, or narrowed by .only still passes CI silently.
const disabling = /\b(?:test|describe)(?:\.describe)?\.(?:skip|fixme|fail|only)\s*\(/;

export function compareJourneys(agentsMarkdown, specSources) {
  const listed = listedJourneys(agentsMarkdown);
  const tagged = taggedJourneys(specSources);
  const disabled = Object.entries(specSources)
    .filter(([, source]) => taggedJourneys({ source }).length > 0 && disabling.test(source)).map(([name]) => name);
  return { missing: listed.filter((title) => !tagged.includes(title)), unlisted: tagged.filter((title) => !listed.includes(title)), disabled };
}

export async function checkOnboardingJourneys() {
  const agents = await readFile(path.join(webDirectory, '..', 'AGENTS.md'), 'utf8');
  const e2e = path.join(webDirectory, 'e2e');
  const names = (await readdir(e2e)).filter((name) => name.endsWith('.spec.ts'));
  const sources = Object.fromEntries(await Promise.all(names.map(async (name) => [name, await readFile(path.join(e2e, name), 'utf8')])));
  return compareJourneys(agents, sources);
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const { missing, unlisted, disabled } = await checkOnboardingJourneys();
  for (const title of missing) console.error(`AGENTS.md lists an onboarding journey with no @onboarding test: ${title}`);
  for (const title of unlisted) console.error(`@onboarding test missing from the AGENTS.md list: ${title}`);
  for (const name of disabled) console.error(`${name} holds onboarding journeys and skips, fixmes, fails or .only's a test`);
  process.exit(missing.length + unlisted.length + disabled.length > 0 ? 1 : 0);
}
