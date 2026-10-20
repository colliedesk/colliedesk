import { AttachmentsTooLargeError, AttachmentStorageError } from '../../api/attachments-error';
import { InvalidRequestError, PlanLimitError } from '../../api/client';
import type { KBCollection } from './kb-api';

// Collections nest three levels deep, like a Freshdesk category, folder and subfolder.
export const maxDepth = 3;

export interface TreeNode {
  collection: KBCollection;
  depth: number;
  children: TreeNode[];
}

// buildTree nests the flat list the API returns (ordered by parent, position and name), keeping that order.
// A collection whose parent is not in the list (it should not happen) is shown at the top, never hidden.
export function buildTree(collections: KBCollection[]): TreeNode[] {
  const byParent = new Map<string | null, KBCollection[]>();
  const ids = new Set(collections.map((item) => item.id));
  for (const item of collections) {
    const parent = item.parent_id && ids.has(item.parent_id) ? item.parent_id : null;
    byParent.set(parent, [...(byParent.get(parent) ?? []), item]);
  }
  const seen = new Set<string>();
  const nodes = (parent: string | null, depth: number): TreeNode[] => (byParent.get(parent) ?? []).filter((item) => !seen.has(item.id)).map((item) => {
    seen.add(item.id);
    return { collection: item, depth, children: nodes(item.id, depth + 1) };
  });
  return nodes(null, 1);
}

// flatten lists the tree in reading order, for a select and for lookups.
export function flatten(tree: TreeNode[]): TreeNode[] {
  return tree.flatMap((node) => [node, ...flatten(node.children)]);
}

// height counts the levels of a collection and the collections inside it: 1 when it holds none.
export function height(node: TreeNode): number {
  return 1 + Math.max(0, ...node.children.map(height));
}

// parentChoices are the collections node may move into: not itself or one inside it, and not so deep
// that it, or a collection inside it, would pass the third level. Without node, any collection that can
// still hold one.
export function parentChoices(tree: TreeNode[], node?: TreeNode): TreeNode[] {
  const inside = new Set(node ? flatten([node]).map((item) => item.collection.id) : []);
  const levels = node ? height(node) : 1;
  return flatten(tree).filter((item) => !inside.has(item.collection.id) && item.depth + levels <= maxDepth);
}

// indent shows a collection's level in a select, where nesting cannot be drawn.
export const indent = (node: TreeNode) => `${' '.repeat(node.depth - 1)}${node.collection.name}`;

// reason is the server's explanation of a refusal when there is one, and fallback otherwise.
export function reason(error: unknown, fallback: string): string {
  return error instanceof InvalidRequestError || error instanceof PlanLimitError || error instanceof AttachmentsTooLargeError
    || error instanceof AttachmentStorageError ? error.message : fallback;
}
