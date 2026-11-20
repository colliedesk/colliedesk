import { escapeLineStart, inlineMarkdown, LINE_BREAK } from './paste-html-inline';

// HTML from the clipboard to the Markdown an article stores. The rules are the Freshdesk import's
// (server/internal/freshdesk), checked against the same cases
// (server/internal/freshdesk/testdata/html-markdown-cases.json): headings, emphasis, lists, links,
// tables and code stay; fonts, sizes, colours and underline leave. A paste is stricter than an
// import: players, frames and images with an unsafe address are left out.
export type PastedMarkdown = {
  markdown: string;
  // Absolute image addresses (web, blob:, data:) to store in the media library, once each.
  images: string[];
};

const DROPPED = 'script,style,template,noscript,iframe,video,audio,embed,object,form,input,button,select,'
  + 'textarea,svg,canvas,math,meta,link,title';
const BLOCK = new Set(['P', 'DIV', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'PRE',
  'TABLE', 'HR', 'SECTION', 'ARTICLE', 'HEADER', 'FOOTER', 'MAIN', 'NAV', 'ASIDE', 'FIGURE', 'FIGCAPTION',
  'DL', 'DT', 'DD', 'ADDRESS', 'DETAILS', 'SUMMARY', 'CENTER']);
const BLOCK_SELECTOR = [...BLOCK].join(',').toLowerCase();
const ELEMENT = 1;

export function pastedMarkdown(html: string): PastedMarkdown {
  // A parsed document runs no script and loads no image, so nothing in the paste executes here.
  const body = new DOMParser().parseFromString(html, 'text/html').body;
  body.querySelectorAll(DROPPED).forEach((el) => el.remove());
  styledRunsToTags(body);
  const images = new Set<string>();
  return { markdown: blocks(body, images).join('\n\n'), images: [...images] };
}

// Word processors (Google Docs, Word) mark bold and italic with styled spans, and Google Docs
// wraps the whole copy in a <b> that is not bold.
function styledRunsToTags(root: HTMLElement) {
  for (const el of [...root.querySelectorAll<HTMLElement>('b, strong')]) {
    if (/^(normal|lighter|[1-5]00)$/.test(el.style.fontWeight)) el.replaceWith(...el.childNodes);
  }
  for (const span of [...root.querySelectorAll<HTMLElement>('span[style]')]) {
    if (span.style.fontStyle === 'italic') wrapChildren(span, 'em');
    if (/^(bold|bolder|[6-9]00)$/.test(span.style.fontWeight)) wrapChildren(span, 'strong');
  }
}

function wrapChildren(el: HTMLElement, tag: string) {
  const wrapper = el.ownerDocument.createElement(tag);
  wrapper.append(...el.childNodes);
  el.append(wrapper);
}

function isElement(node: Node): node is Element {
  return node.nodeType === ELEMENT;
}

function holdsBlocks(el: Element): boolean {
  return BLOCK.has(el.tagName) || el.querySelector(BLOCK_SELECTOR) !== null;
}

function blocks(parent: Node, images: Set<string>): string[] {
  const out: string[] = [];
  let run: Node[] = [];
  const flush = () => {
    const text = paragraph(run, images);
    if (text) out.push(text);
    run = [];
  };
  for (const node of parent.childNodes) {
    if (isElement(node) && holdsBlocks(node)) {
      flush();
      out.push(...block(node, images));
    } else {
      run.push(node);
    }
  }
  flush();
  return out.filter(Boolean);
}

function paragraph(nodes: Node[], images: Set<string>): string {
  const lines = inlineMarkdown(nodes, images).replace(/ {2,}/g, ' ').split(LINE_BREAK)
    .map((line) => line.replace(/^ +| +$/g, ''));
  while (lines.length && !lines[0]) lines.shift();
  while (lines.length && !lines[lines.length - 1]) lines.pop();
  return lines.map(escapeLineStart).join('  \n');
}

function block(el: Element, images: Set<string>): string[] {
  const heading = /^H([1-6])$/.exec(el.tagName);
  if (heading) {
    const text = paragraph([...el.childNodes], images).replace(/ {2}\n/g, ' ');
    return text ? [`${'#'.repeat(Number(heading[1]))} ${text}`] : [];
  }
  switch (el.tagName) {
    case 'UL': case 'OL': return [list(el, images)];
    case 'BLOCKQUOTE': {
      const inner = blocks(el, images).join('\n\n');
      return inner ? [inner.split('\n').map((line) => (line ? `> ${line}` : '>')).join('\n')] : [];
    }
    case 'PRE': return [fence(el)];
    case 'TABLE': return [table(el as HTMLTableElement, images)];
    case 'HR': return ['* * *'];
    default: return blocks(el, images);
  }
}

function list(el: Element, images: Set<string>): string {
  const ordered = el.tagName === 'OL';
  const start = Number.parseInt(el.getAttribute('start') ?? '', 10);
  let number = Number.isNaN(start) ? 1 : start;
  // A nested list written next to its item (<ul><li>a</li><ul>…</ul></ul>) belongs to that item.
  const items: Element[][] = [];
  for (const child of el.children) {
    if (child.tagName === 'LI' || !items.length) items.push([child]);
    else items[items.length - 1].push(child);
  }
  return items.map((parts) => {
    const marker = ordered ? `${number++}. ` : '- ';
    const body = parts.flatMap((part) => (part.tagName === 'LI' ? blocks(part, images) : block(part, images)));
    const [first = '', ...rest] = body.join('\n\n').split('\n');
    return [marker + first, ...rest.map((line) => ' '.repeat(marker.length) + line)].join('\n');
  }).join('\n');
}

function fence(pre: Element): string {
  pre.querySelectorAll('br').forEach((br) => br.replaceWith('\n'));
  const code = pre.querySelector('code');
  const language = /(?:^|\s)lang(?:uage)?-([\w+#.-]+)/.exec(code?.className || pre.className)?.[1] ?? '';
  const text = (pre.textContent ?? '').replace(/\n$/, '');
  const longest = Math.max(0, ...(text.match(/`{3,}/g) ?? []).map((run) => run.length));
  const ticks = '`'.repeat(Math.max(3, longest + 1));
  return `${ticks}${language}\n${text}\n${ticks}`;
}

function table(el: HTMLTableElement, images: Set<string>): string {
  const rows = [...el.rows].map((row) => ({
    header: row.parentElement?.tagName === 'THEAD' || [...row.cells].some((cell) => cell.tagName === 'TH'),
    cells: [...row.cells].map((cell) => blocks(cell, images).join(' ').replace(/\s*\n\s*/g, ' ').replace(/\|/g, '\\|')),
  }));
  if (!rows.length) return '';
  const head = rows[0].header ? rows.shift()!.cells : [];
  const columns = Math.max(head.length, ...rows.map((row) => row.cells.length));
  const all = [head, ...rows.map((row) => row.cells)];
  const widths = Array.from({ length: columns }, (_, i) => Math.max(0, ...all.map((cells) => [...(cells[i] ?? '')].length)));
  const line = (cells: string[]) => `| ${widths.map((w, i) => pad(cells[i] ?? '', w)).join(' | ')} |`;
  const rule = `|${widths.map((w) => '-'.repeat(w + 2)).join('|')}|`;
  return [line(head), rule, ...rows.map((row) => line(row.cells))].join('\n');
}

function pad(text: string, width: number): string {
  return text + ' '.repeat(width - [...text].length);
}
