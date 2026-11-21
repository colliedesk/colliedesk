import { describe, expect, it } from 'vitest';
import cases from '../../../../../server/internal/kbformat/testdata/normalize.json';
import { parseTag, parseTags, tagString } from './format-tags';

const tags = (text: string) => text.match(/<\/?[A-Za-z][^>]*>/g) ?? [];

// The server's normalizer cases (server/internal/kbformat/normalize_test.go): each tag it rewrites, the
// editor writes the same way, so a body the editor saves comes back unchanged.
describe('the editor spells each formatting tag as the server does', () => {
  it.each(cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const before = tags(c.in);
    const after = tags(c.out);
    expect(after).toHaveLength(before.length);
    before.forEach((tag, i) => {
      if (tag !== after[i]) expect(tagString(parseTag(tag)!)).toBe(after[i]);
    });
  });

  it('a canonical tag is written back as it is', () => {
    const canonical = cases.find((c) => c.in === c.out && c.in.includes('<table>'))!;
    for (const tag of tags(canonical.out)) expect(tagString(parseTag(tag)!)).toBe(tag);
  });

  it.each([
    ['text between tags', '<u>x</u>'],
    ['a self-closing tag', '<span data-color="accent"/>'],
    ['a comment', '<!-- note -->'],
    ['another element', '<div>'],
  ])('%s is not a run of formatting tags', (_name, raw) => {
    expect(parseTags(raw)).toBeUndefined();
  });
});
