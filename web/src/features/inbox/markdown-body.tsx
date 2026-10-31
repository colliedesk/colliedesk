import { lazyPart } from '../../ui/lazy-part';
import { PlainTextBody } from './plain-text-body';

// The renderer loads on its own; the inbox starts it when it opens, so it is there before a conversation is.
// Until it is (or if its chunk fails), the words show as plain text: never an empty message.
export const markdownPart = lazyPart(() => import('./markdown-render'));

export function MarkdownBody({ text }: { text: string }) {
  const state = markdownPart.use();
  return state.status === 'ready' ? <state.module.MarkdownRender text={text} /> : <PlainTextBody text={text} />;
}
