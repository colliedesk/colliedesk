import { blockClass } from './editor/format-tags';
import { formatTree } from './editor/format-tree';
import type { FormatNode } from './editor/format-tree';
import './editor/formatting.css';

// formatting shows an article's formatting HTML in the preview as the help center renders it
// (server/internal/kbformat): <u>, <mark>, <span class="kb-color-…">, kb-align/kb-indent classes on a
// wrapped paragraph or heading, and an HTML table with colspan, rowspan and kb-bg classes. A span with no
// colour of the palette shows its text only.
export function formatting() {
  return (tree: FormatNode) => {
    formatTree(tree);
    decorate(tree);
  };
}

const withClass = (properties: Record<string, unknown>, classes: string[]) => (classes.length > 0 ? { ...properties, className: classes } : properties);

function decorate(parent: FormatNode) {
  if (!parent.children) return;
  parent.children = parent.children.flatMap((child): FormatNode[] => {
    decorate(child);
    const tag = child.tag;
    switch (child.type) {
      case 'kbInline': {
        if (tag!.name !== 'span') return [{ ...child, data: { hName: tag!.name } }];
        const color = tag!.attrs['data-color'];
        return color ? [{ ...child, data: { hName: 'span', hProperties: { className: [`kb-color-${color}`] } } }] : child.children!;
      }
      case 'kbTable':
        return [{ ...child, data: { hName: 'table' }, children: [{ type: 'kbBody', data: { hName: 'tbody' }, children: child.children }] }];
      case 'kbRow':
        return [{ ...child, data: { hName: 'tr' } }];
      case 'kbCell': {
        const span: Record<string, unknown> = {};
        if (tag!.attrs.colspan) span.colSpan = Number(tag!.attrs.colspan);
        if (tag!.attrs.rowspan) span.rowSpan = Number(tag!.attrs.rowspan);
        return [{ ...child, data: { hName: tag!.name, hProperties: withClass(span, blockClass(tag!)) } }];
      }
    }
    const wrap = child.data?.kbWrap;
    if (wrap && blockClass(wrap).length > 0) {
      child.data = { ...child.data, hProperties: withClass(child.data?.hProperties ?? {}, blockClass(wrap)) };
    }
    return [child];
  });
}
