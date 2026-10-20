import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { kbMessages } from './kb-messages';

const plugins = [remarkGfm];

// Only images uploaded to the knowledge base are shown, as on the help center (its pages allow images from
// their own site only); an image from another site would also tell that site who is reading the preview.
const components: Components = {
  img: ({ src, alt }) => (typeof src === 'string' && src.startsWith('/api/v1/kb/images/')
    ? <img src={src} alt={alt ?? ''} />
    : <span className="kb-external-image">{kbMessages.externalImage(alt ?? '', typeof src === 'string' ? src : '')}</span>),
};

// MarkdownPreview shows an article as Markdown with tables, task lists and strikethrough (GFM). Raw HTML
// is left out, never rendered, as on the help center; react-markdown also drops unsafe link targets.
export function MarkdownPreview({ text }: { text: string }) {
  if (!text.trim()) return <p className="empty-copy">{kbMessages.nothingToPreview}</p>;
  return <div className="markdown-body kb-preview"><Markdown remarkPlugins={plugins} components={components}>{text}</Markdown></div>;
}
