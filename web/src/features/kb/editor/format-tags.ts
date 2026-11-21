import allowlist from '../../../../../server/internal/kbhtml/allowlist.json';

// The formatting tags an article may hold (allowlist.json "formatting"), read and written as the server's
// kbformat package does: the preview shows them as the help center does, and the visual editor writes each
// tag in the one spelling the server keeps, so a saved text never comes back changed.

export const rules = allowlist.formatting;
export type Palette = (typeof rules.colors)[number];
export type Align = (typeof rules.align)[number];

// attributeOrder is the order a canonical tag writes its attributes in.
export const attributeOrder = ['colspan', 'rowspan', 'data-align', 'data-indent', 'data-color', 'data-bg'] as const;
export type Attribute = (typeof attributeOrder)[number];

export interface FormatTag {
  name: string;
  close: boolean;
  attrs: Partial<Record<Attribute, string>>;
}

const marks: readonly string[] = rules.marks;
const blocks: readonly string[] = rules.blocks;
const colors: readonly string[] = rules.colors;
const aligns: readonly string[] = rules.align;

export const isInline = (tag: FormatTag) => tag.name === 'span' || marks.includes(tag.name);
export const isBlock = (tag: FormatTag) => blocks.includes(tag.name);
export const isCell = (tag: FormatTag) => tag.name === 'th' || tag.name === 'td';
export const isTable = (tag: FormatTag) => tag.name === 'table' || tag.name === 'tr' || isCell(tag);

// within reports whether value is a plain decimal number from low to high.
const within = (value: string, low: number, high: number) => /^(0|[1-9][0-9]*)$/.test(value) && Number(value) >= low && Number(value) <= high;

function allowed(name: string, key: string, value: string) {
  const cell = name === 'th' || name === 'td';
  const block = blocks.includes(name);
  switch (key) {
    case 'data-color': return name === 'span' && colors.includes(value);
    case 'data-bg': return cell && colors.includes(value);
    case 'data-align': return (cell || block) && aligns.includes(value);
    case 'data-indent': return block && within(value, 1, rules.indent);
    // 1 is every cell's span already, so it is never written.
    case 'colspan': case 'rowspan': return cell && within(value, 2, rules.span);
  }
  return false;
}

const tagPattern = /<(\/?)([A-Za-z][A-Za-z0-9-]*)((?:[\t\n\f\r ]+[^\t\n\f\r "'>/=]+(?:[\t\n\f\r ]*=[\t\n\f\r ]*(?:"[^"]*"|'[^']*'|[^\t\n\f\r "'=<>`]+))?)*)[\t\n\f\r ]*>/y;
const attributePattern = /([^\t\n\f\r "'>/=]+)(?:[\t\n\f\r ]*=[\t\n\f\r ]*(?:"([^"]*)"|'([^']*)'|([^\t\n\f\r "'=<>`]+)))?/g;

// parseTags reads raw as a run of formatting tags parted by whitespace only, keeping the attributes and
// values each element allows; undefined for anything else (text, a comment, a self-closing or other tag).
export function parseTags(raw: string): FormatTag[] | undefined {
  const tags: FormatTag[] = [];
  let at = 0;
  for (;;) {
    while (at < raw.length && /\s/.test(raw[at]!)) at++;
    if (at === raw.length) return tags.length > 0 ? tags : undefined;
    tagPattern.lastIndex = at;
    const match = tagPattern.exec(raw);
    if (!match) return undefined;
    at = tagPattern.lastIndex;
    const tag: FormatTag = { name: match[2]!.toLowerCase(), close: match[1] === '/', attrs: {} };
    if (!isInline(tag) && !isBlock(tag) && !isTable(tag)) return undefined;
    if (!tag.close) tag.attrs = kept(tag.name, match[3]!);
    tags.push(tag);
  }
}

// parseTag reads raw as exactly one formatting tag.
export function parseTag(raw: string): FormatTag | undefined {
  const tags = parseTags(raw);
  return tags?.length === 1 ? tags[0] : undefined;
}

function kept(name: string, source: string): FormatTag['attrs'] {
  const given: [string, string][] = [];
  for (const match of source.matchAll(attributePattern)) given.push([match[1]!.toLowerCase(), match[2] ?? match[3] ?? match[4] ?? '']);
  const attrs: FormatTag['attrs'] = {};
  for (const key of attributeOrder) {
    const value = given.find(([k, v]) => k === key && allowed(name, key, v));
    if (value) attrs[key] = value[1];
  }
  return attrs;
}

// tagString is the tag's canonical spelling, as the server's Tag.String writes it.
export function tagString(tag: FormatTag) {
  if (tag.close) return `</${tag.name}>`;
  const attrs = attributeOrder.filter((key) => tag.attrs[key] !== undefined).map((key) => ` ${key}="${tag.attrs[key]}"`);
  return `<${tag.name}${attrs.join('')}>`;
}

// blockClass is the help center's classes for a wrapper's or a cell's attributes.
export function blockClass(tag: FormatTag): string[] {
  const { 'data-align': align, 'data-indent': indent, 'data-bg': bg } = tag.attrs;
  return [align && `kb-align-${align}`, indent && `kb-indent-${indent}`, bg && `kb-bg-${bg}`].filter((c): c is string => !!c);
}
