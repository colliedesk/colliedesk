import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { fragment } from './kb-html';
import type { HtmlNode } from './kb-html';
import { kbMessages } from './kb-messages';

const plugins = [remarkGfm];

interface Tree {
  type: string;
  value?: string;
  children?: Tree[];
}

// articleHtml replaces each piece of raw HTML with what the help center shows of it (kb-html.ts) and puts a
// line on top that names what was left out.
function articleHtml() {
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
        return shown.nodes satisfies HtmlNode[];
      });
    };
    visit(tree);
    const waiting = dropped.includes('video (media library)');
    const others = dropped.filter((what) => what !== 'video (media library)');
    if (!waiting && others.length === 0) return;
    const text = [others.length ? kbMessages.htmlLeftOut(others.join(', ')) : '', waiting ? kbMessages.libraryVideoLater : '']
      .filter(Boolean)
      .join(' ');
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
export function MarkdownPreview({ text }: { text: string }) {
  if (!text.trim()) return <p className="empty-copy">{kbMessages.nothingToPreview}</p>;
  return <div className="markdown-body kb-preview"><Markdown remarkPlugins={plugins} rehypePlugins={rehypePlugins} components={components}>{text}</Markdown></div>;
}
