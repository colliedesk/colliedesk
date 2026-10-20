import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { kbMessages } from './kb-messages';

const plugins = [remarkGfm];

// MarkdownPreview shows an article as Markdown with tables, task lists and strikethrough (GFM). Raw HTML
// is left out, never rendered, as on the help center; react-markdown also drops unsafe link targets.
export function MarkdownPreview({ text }: { text: string }) {
  if (!text.trim()) return <p className="empty-copy">{kbMessages.nothingToPreview}</p>;
  return <div className="markdown-body kb-preview"><Markdown remarkPlugins={plugins}>{text}</Markdown></div>;
}
