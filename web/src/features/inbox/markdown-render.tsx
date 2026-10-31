import Markdown from 'react-markdown';

// The Markdown renderer of replies and notes: its own chunk, since react-markdown and micromark are about a
// quarter of what the first bundle would carry otherwise (docs/internals/performance.md, Web bundle budget).
export function MarkdownRender({ text }: { text: string }) {
  return <Markdown>{text}</Markdown>;
}
