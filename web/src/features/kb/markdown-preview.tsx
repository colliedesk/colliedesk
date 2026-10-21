import { useMemo } from 'react';
import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { closes, fragment, hidesWhatFollows } from './kb-html';
import type { HtmlNode } from './kb-html';
import { kbMessages } from './kb-messages';

// Blocks whose raw HTML is an HTML block of its own; anywhere else it is inline.
const blockParents = ['root', 'listItem', 'blockquote', 'footnoteDefinition'];

// hiddenText removes what inline raw HTML hides, as the help center does: the words between an inline
// <script>, <style>, <iframe>… and its closing tag (or the end of the paragraph).
function hiddenText() {
  const visit = (parent: Tree) => {
    if (!parent.children) return;
    if (!blockParents.includes(parent.type)) {
      let hidden = '';
      parent.children = parent.children.filter((child) => {
        if (hidden) {
          if (child.type === 'html' && closes(child.value ?? '', hidden)) hidden = '';
          return false;
        }
        if (child.type === 'html') hidden = hidesWhatFollows(child.value ?? '');
        return true;
      });
    }
    for (const child of parent.children) visit(child);
  };
  return (tree: Tree) => visit(tree);
}

const plugins = [remarkGfm, hiddenText];

interface Tree {
  type: string;
  tagName?: string;
  value?: string;
  children?: Tree[];
}

// PreviewVideo is what the library says of a video an article shows: its poster and size, or null when the
// library has no such video (the help center leaves it out). A video not looked up yet is shown as it is.
export type PreviewVideo = { poster: string | null; width: number | null; height: number | null } | null;
export type PreviewLibrary = ReadonlyMap<string, PreviewVideo>;

const mediaPrefix = '/api/v1/kb/media/';

// articleHtml replaces each piece of raw HTML with what the help center shows of it (kb-html.ts), drops a
// paragraph left empty and puts a line on top that names what was left out. With a library, a video gets
// its poster and size, and one the library does not have is left out, as on the help center.
function articleHtml(options: { library?: PreviewLibrary } = {}) {
  const library = options.library;
  const withLibrary = (nodes: HtmlNode[], dropped: string[]) => nodes.flatMap((node) => {
    if (node.tagName !== 'video' || !library) return [node];
    const id = String(node.properties.src).slice(mediaPrefix.length);
    if (!library.has(id)) return [node];
    const video = library.get(id);
    if (!video) {
      if (!dropped.includes('video (not in the library)')) dropped.push('video (not in the library)');
      return [];
    }
    const properties = { ...node.properties };
    if (video.poster) properties.poster = video.poster;
    if (properties.width === undefined && properties.height === undefined && video.width && video.height) {
      properties.width = String(video.width);
      properties.height = String(video.height);
    }
    return [{ ...node, properties }];
  });
  return (tree: Tree) => {
    const dropped: string[] = [];
    const visit = (parent: Tree) => {
      if (!parent.children) return;
      parent.children = parent.children.flatMap((child): Tree[] => {
        if (child.type !== 'raw') {
          visit(child);
          return [child];
        }
        const shown = fragment(child.value ?? '');
        for (const what of shown.dropped) if (!dropped.includes(what)) dropped.push(what);
        return withLibrary(shown.nodes, dropped) satisfies HtmlNode[];
      });
      // A paragraph left with nothing to show goes, as on the help center.
      parent.children = parent.children.filter(
        (child) => child.tagName !== 'p' || (child.children ?? []).some((inline) => inline.type !== 'text' || (inline.value ?? '').trim() !== ''),
      );
    };
    visit(tree);
    if (dropped.length === 0) return;
    const text = kbMessages.htmlLeftOut(dropped.join(', '));
    tree.children?.unshift({
      type: 'element',
      tagName: 'p',
      properties: { className: ['kb-html-warning'], role: 'status' },
      children: [{ type: 'text', value: text }],
    } as Tree);
  };
}
const rehypePlugins = [articleHtml];

// Only images uploaded to the knowledge base are shown, as on the help center (its pages allow images from
// their own site only); an image from another site would also tell that site who is reading the preview.
const components: Components = {
  img: ({ src, alt }) => (typeof src === 'string' && src.startsWith('/api/v1/kb/images/')
    ? <img src={src} alt={alt ?? ''} />
    : <span className="kb-external-image">{kbMessages.externalImage(alt ?? '', typeof src === 'string' ? src : '')}</span>),
};

// MarkdownPreview shows an article as Markdown with tables, task lists and strikethrough (GFM). Raw HTML
// shows only YouTube and Vimeo players, as on the help center; react-markdown also drops unsafe link targets.
export function MarkdownPreview({ text, library }: { text: string; library?: PreviewLibrary }) {
  const withLibrary = useMemo(() => (library ? [[articleHtml, { library }] as [typeof articleHtml, { library: PreviewLibrary }]] : rehypePlugins), [library]);
  if (!text.trim()) return <p className="empty-copy">{kbMessages.nothingToPreview}</p>;
  return <div className="markdown-body kb-preview"><Markdown remarkPlugins={plugins} rehypePlugins={withLibrary} components={components}>{text}</Markdown></div>;
}

// videoIds lists the library videos a text names, at most limit of them.
export function videoIds(text: string, limit = 20): string[] {
  const ids = new Set<string>();
  for (const match of text.matchAll(/\/api\/v1\/kb\/media\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/g)) {
    ids.add(match[1]!);
    if (ids.size >= limit) break;
  }
  return [...ids];
}
