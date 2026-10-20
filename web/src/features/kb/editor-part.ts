import { lazyPart } from '../../ui/lazy-part';

// The Markdown editor (CodeMirror) is a chunk of its own: the article list never loads it. An article page loads
// it as it opens, alongside the article; a pointer or the focus on the list or on New article preloads it.
export const editorPart = lazyPart(() => import('./editor/codemirror-editor'));
