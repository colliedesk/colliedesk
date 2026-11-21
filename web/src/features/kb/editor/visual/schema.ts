import { getSchema } from '@tiptap/core';
import Blockquote from '@tiptap/extension-blockquote';
import Bold from '@tiptap/extension-bold';
import CodeBlock from '@tiptap/extension-code-block';
import HardBreak from '@tiptap/extension-hard-break';
import Heading from '@tiptap/extension-heading';
import HorizontalRule from '@tiptap/extension-horizontal-rule';
import Image from '@tiptap/extension-image';
import Italic from '@tiptap/extension-italic';
import { BulletList, ListItem, OrderedList, TaskItem, TaskList } from '@tiptap/extension-list';
import Paragraph from '@tiptap/extension-paragraph';
import Strike from '@tiptap/extension-strike';
import { Table, TableRow } from '@tiptap/extension-table';
import Text from '@tiptap/extension-text';
import Underline from '@tiptap/extension-underline';
import { Color, Highlight, InlineCode, Link } from './marks';
import { ArticleCell, ArticleDocument, ArticleHeader, BlockFormat, Callout, ListSpacing, MarkdownBlock } from './nodes';

// articleContent is every node and mark an article can hold, and nothing more: what the visual editor
// shows is exactly what it can save (Markdown, GFM and the server's formatting tags).
export const articleContent = [
  ArticleDocument, Paragraph, Text, Heading, Blockquote, CodeBlock, HorizontalRule, HardBreak,
  Image.configure({ inline: true }),
  BulletList, OrderedList, ListItem, TaskList, TaskItem.configure({ nested: true }),
  Table.configure({ resizable: false }), TableRow, ArticleHeader, ArticleCell,
  Callout, MarkdownBlock, BlockFormat, ListSpacing,
  Link, Color, Highlight, Underline, Bold, Italic, Strike, InlineCode,
];

export const schema = getSchema(articleContent);
