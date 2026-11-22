// Keeps each journey list in docs/internals/journeys.md ("Onboarding journeys", "Daily-use journeys") and the end-to-end tests
// in web/e2e carrying its tag identical, so a journey cannot be deleted, renamed, untagged, skipped or narrowed
// by .only (and so dropped from its CI job) without the list, and the people reading it, noticing.
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const webDirectory = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

export const journeyLists = [
  { section: 'Onboarding journeys', tag: '@onboarding' },
  { section: 'Daily-use journeys', tag: '@journey' },
];

function listedJourneys(listMarkdown, section) {
  const part = listMarkdown.split(/^## /m).find((heading) => heading.startsWith(section)) ?? '';
  return [...part.matchAll(/^- `([^`]+)`/gm)].map((match) => match[1]);
}

function taggedJourneys(specSources, tag) {
  const pattern = new RegExp(String.raw`\btest\('((?:[^'\\]|\\.)*)',\s*\{\s*tag:\s*'${tag}'`, 'g');
  return Object.values(specSources).flatMap((source) => [...source.matchAll(pattern)].map((match) => match[1].replace(/\\(.)/g, '$1')));
}

// A journey that is skipped, marked fixme/fail, or narrowed by .only still passes CI silently.
const disabling = /\b(?:test|describe)(?:\.describe)?\.(?:skip|fixme|fail|only)\s*\(/;

export function compareJourneys(listMarkdown, specSources, { section, tag }) {
  const listed = listedJourneys(listMarkdown, section);
  const tagged = taggedJourneys(specSources, tag);
  const disabled = Object.entries(specSources)
    .filter(([, source]) => taggedJourneys({ source }, tag).length > 0 && disabling.test(source)).map(([name]) => name);
  return { missing: listed.filter((title) => !tagged.includes(title)), unlisted: tagged.filter((title) => !listed.includes(title)), disabled };
}

export const listFile = 'docs/internals/journeys.md';

export async function checkJourneys() {
  const list = await readFile(path.join(webDirectory, '..', listFile), 'utf8');
  const e2e = path.join(webDirectory, 'e2e');
  const names = (await readdir(e2e)).filter((name) => name.endsWith('.spec.ts'));
  const sources = Object.fromEntries(await Promise.all(names.map(async (name) => [name, await readFile(path.join(e2e, name), 'utf8')])));
  return Object.fromEntries(journeyLists.map((entry) => [entry.tag, compareJourneys(list, sources, entry)]));
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const results = await checkJourneys();
  let problems = 0;
  for (const { section, tag } of journeyLists) {
    const { missing, unlisted, disabled } = results[tag];
    for (const title of missing) console.error(`${listFile} "${section}" lists a journey with no ${tag} test: ${title}`);
    for (const title of unlisted) console.error(`${tag} test missing from the ${listFile} "${section}" list: ${title}`);
    for (const name of disabled) console.error(`${name} holds ${tag} journeys and skips, fixmes, fails or .only's a test`);
    problems += missing.length + unlisted.length + disabled.length;
  }
  process.exit(problems > 0 ? 1 : 0);
}
