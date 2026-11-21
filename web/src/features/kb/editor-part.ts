import { useQueryClient } from '@tanstack/react-query';
import type { Me } from '../../api/schemas';
import { lazyPart } from '../../ui/lazy-part';

// Each editor is a chunk of its own: the article list never loads one. An article page loads the one its member
// writes in as it opens, alongside the article; a pointer or the focus on the list or on New article preloads it.
export const editorPart = lazyPart(() => import('./editor/codemirror-editor'));
export const visualPart = lazyPart(() => import('./editor/visual/visual-editor'));

// preloadEditor starts loading the editor of a member's choice (Me.kb_editor): the visual one until they choose,
// and both while the page does not know yet who is signed in (undefined).
export function preloadEditor(choice: Me['kb_editor'] | undefined) {
  if (choice !== 'markdown') visualPart.preload();
  if (choice !== 'visual' && choice !== null) editorPart.preload();
}

// usePreloadEditor is preloadEditor for the signed-in member, for a pointer or the focus to call.
export function usePreloadEditor() {
  const client = useQueryClient();
  return () => {
    const me = client.getQueryData<Me>(['me']);
    preloadEditor(me ? me.kb_editor : undefined);
  };
}
