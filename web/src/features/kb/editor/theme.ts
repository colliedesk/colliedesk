import { HighlightStyle, Language, defineLanguageFacet, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { GFM, parser } from '@lezer/markdown';

// @codemirror/lang-markdown imports lang-html, lang-css and lang-javascript statically (+74 KB gzip) for code
// blocks. The bare GFM parser highlights the Markdown itself, which is all an article needs.
export const markdownLanguage = new Language(defineLanguageFacet(), parser.configure(GFM), [], 'markdown');

// Colours are the app's tokens, so the same style serves light and dark.
export const markdownHighlight = syntaxHighlighting(
  HighlightStyle.define([
    { tag: tags.heading1, fontWeight: '700', fontSize: '1.2em', color: 'var(--text-1)' },
    { tag: tags.heading2, fontWeight: '700', fontSize: '1.1em', color: 'var(--text-1)' },
    { tag: [tags.heading3, tags.heading4, tags.heading5, tags.heading6], fontWeight: '700', color: 'var(--text-1)' },
    { tag: tags.strong, fontWeight: '700' },
    { tag: tags.emphasis, fontStyle: 'italic' },
    { tag: tags.strikethrough, textDecoration: 'line-through' },
    { tag: tags.link, color: 'var(--accent-text)' },
    { tag: tags.url, color: 'var(--accent-text)', textDecoration: 'underline', textUnderlineOffset: '2px' },
    // Code is coloured, not shaded: a shade drawn per line breaks up in a wrapped code block.
    { tag: tags.monospace, color: 'var(--text-2)' },
    { tag: [tags.processingInstruction, tags.contentSeparator, tags.labelName], color: 'var(--text-3)' },
    { tag: tags.quote, color: 'var(--text-2)' },
    { tag: tags.atom, color: 'var(--accent-text)', fontWeight: '600' },
    { tag: tags.comment, color: 'var(--text-3)', fontStyle: 'italic' },
  ]),
);
