import allowlist from '../../../../../server/internal/kbhtml/allowlist.json';

// The formatting tags an article may hold (allowlist.json "formatting"), read and written as the server's
// kbformat package does: the preview shows them as the help center does, and the visual editor writes each
// tag in the one spelling the server keeps, so a saved text never comes back changed.

export const rules = allowlist.formatting;
export type Palette = (typeof rules.colors)[number];
export type Align = (typeof rules.align)[number];

// attributeOrder is the order a canonical tag writes its attributes in.
export const attributeOrder = ['colspan', 'rowspan', 'data-width', 'data-align', 'data-indent', 'data-color', 'data-bg', 'data-border'] as const;
export type Attribute = (typeof attributeOrder)[number];
export type FigureWidth = (typeof rules.figure.widths)[number];
export type FigureAlign = (typeof rules.figure.align)[number];

// bare are the attributes whose presence is their value: a canonical tag writes them with none.
const bare: readonly string[] = ['data-border'];

export interface FormatTag {
  name: string;
  close: boolean;
  attrs: Partial<Record<Attribute, string>>;
}

const marks: readonly string[] = rules.marks;
const blocks: readonly string[] = rules.blocks;
const colors: readonly string[] = rules.colors;
const aligns: readonly string[] = rules.align;
const figureWidths: readonly string[] = rules.figure.widths;
const figureAligns: readonly string[] = rules.figure.align;

export const isInline = (tag: FormatTag) => tag.name === 'span' || marks.includes(tag.name);
export const isBlock = (tag: FormatTag) => blocks.includes(tag.name);
export const isCell = (tag: FormatTag) => tag.name === 'th' || tag.name === 'td';
export const isTable = (tag: FormatTag) => tag.name === 'table' || tag.name === 'tr' || isCell(tag);
export const isFigure = (tag: FormatTag) => tag.name === 'figure' || tag.name === 'figcaption';

// within reports whether value is a plain decimal number from low to high.
export const within = (value: string, low: number, high: number) => /^(0|[1-9][0-9]*)$/.test(value) && Number(value) >= low && Number(value) <= high;

function allowed(name: string, key: string, value: string) {
  const cell = name === 'th' || name === 'td';
  const block = blocks.includes(name);
  const figure = name === 'figure';
  switch (key) {
    case 'data-color': return name === 'span' && colors.includes(value);
    case 'data-bg': return cell && colors.includes(value);
    case 'data-align': return ((cell || block) && aligns.includes(value)) || (figure && figureAligns.includes(value));
    case 'data-indent': return block && within(value, 1, rules.indent);
    // 1 is every cell's span already, so it is never written.
    case 'colspan': case 'rowspan': return cell && within(value, 2, rules.span);
    case 'data-width': return figure && (figureWidths.includes(value) || within(value, 1, rules.figure.pixels));
    // A border is on or off, so whatever value it was written with means on.
    case 'data-border': return figure;
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
    if (!isInline(tag) && !isBlock(tag) && !isTable(tag) && !isFigure(tag)) return undefined;
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
    if (value) attrs[key] = bare.includes(key) ? '' : value[1];
  }
  return attrs;
}

// tagString is the tag's canonical spelling, as the server's Tag.String writes it.
export function tagString(tag: FormatTag) {
  if (tag.close) return `</${tag.name}>`;
  const attrs = attributeOrder.filter((key) => tag.attrs[key] !== undefined).map((key) => (bare.includes(key) ? ` ${key}` : ` ${key}="${tag.attrs[key]}"`));
  return `<${tag.name}${attrs.join('')}>`;
}

// blockClass is the help center's classes for a wrapper's or a cell's attributes.
export function blockClass(tag: FormatTag): string[] {
  const { 'data-align': align, 'data-indent': indent, 'data-bg': bg } = tag.attrs;
  return [align && `kb-align-${align}`, indent && `kb-indent-${indent}`, bg && `kb-bg-${bg}`].filter((c): c is string => !!c);
}

// figureClass is the help center's classes for an image's figure: its named width, its alignment and its
// border. A width in pixels is the image's own width attribute instead.
export function figureClass(tag: FormatTag): string[] {
  const { 'data-width': width, 'data-align': align, 'data-border': border } = tag.attrs;
  return ['kb-figure', width && figureWidths.includes(width) && `kb-figure-${width}`, align && `kb-figure-${align}`, border !== undefined && 'kb-figure-border']
    .filter((c): c is string => !!c);
}
