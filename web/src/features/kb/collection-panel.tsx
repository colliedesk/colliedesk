import { useId, useState } from 'react';
import type { FormEvent } from 'react';
import type { KBCollection } from './kb-api';
import { RouterLink } from '../../ui/router-link';
import { kbMessages } from './kb-messages';
import { indent, parentChoices, reason } from './kb-tree';
import type { TreeNode } from './kb-tree';

export interface CollectionValues {
  name: string;
  slug: string;
  description: string;
  parent_id: string | null;
}

interface CollectionFormProps {
  tree: TreeNode[];
  // node is the collection being edited; without it the form creates one inside parentId.
  node?: TreeNode;
  parentId?: string | null;
  onSubmit: (values: CollectionValues) => Promise<unknown>;
  onCancel: () => void;
}

// CollectionForm creates or edits a collection. Changing the address name of an existing one warns that
// its help center addresses change, since old ones are not redirected.
export function CollectionForm({ tree, node, parentId = null, onSubmit, onCancel }: CollectionFormProps) {
  const id = useId();
  const original = node?.collection;
  const [values, setValues] = useState<CollectionValues>({
    name: original?.name ?? '', slug: original?.slug ?? '', description: original?.description ?? '', parent_id: original ? original.parent_id : parentId,
  });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const set = (change: Partial<CollectionValues>) => setValues((current) => ({ ...current, ...change }));
  const slugChanged = original !== undefined && values.slug.trim() !== original.slug;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !values.name.trim()) return;
    setBusy(true);
    setProblem(undefined);
    try {
      await onSubmit({ ...values, name: values.name.trim(), slug: values.slug.trim() });
    } catch (error) {
      setProblem(reason(error, kbMessages.collectionActionFailed));
      setBusy(false);
    }
  };
  const choices = parentChoices(tree, node);
  const label = original ? kbMessages.editCollectionName(original.name) : kbMessages.newCollection;
  return (
    <form className="kb-collection-form" aria-label={label} onSubmit={(event) => void submit(event)}>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <label className="field"><span>{kbMessages.collectionName}</span>
        <input autoFocus required value={values.name} onChange={(event) => set({ name: event.target.value })} /></label>
      {original && <div className="field"><label htmlFor={`${id}-slug-input`}>{kbMessages.collectionSlug}</label>
        <input id={`${id}-slug-input`} value={values.slug} aria-describedby={`${id}-slug`} onChange={(event) => set({ slug: event.target.value })} />
        <small id={`${id}-slug`} className="muted-copy">{kbMessages.collectionSlugHelp}</small></div>}
      {slugChanged && <p className="warning-note" role="note">{kbMessages.slugChangeWarning}</p>}
      <label className="field"><span>{kbMessages.collectionDescription}</span>
        <textarea rows={2} value={values.description} onChange={(event) => set({ description: event.target.value })} /></label>
      <label className="field"><span>{kbMessages.collectionParent}</span>
        <select value={values.parent_id ?? ''} onChange={(event) => set({ parent_id: event.target.value || null })}>
          <option value="">{kbMessages.topLevel}</option>
          {choices.map((choice) => <option key={choice.collection.id} value={choice.collection.id}>{indent(choice)}</option>)}
        </select></label>
      <div className="button-row">
        <button className="primary-action" type="submit" disabled={busy || !values.name.trim()}>{original ? kbMessages.saveCollection : kbMessages.createCollection}</button>
        <button type="button" disabled={busy} onClick={onCancel}>{kbMessages.cancel}</button>
      </div>
    </form>
  );
}

interface CollectionTreeProps {
  tree: TreeNode[];
  selected?: string;
  onSelect: (collection?: KBCollection) => void;
}

// CollectionTree lists the collections as nested links; the chosen one filters the articles.
export function CollectionTree({ tree, selected, onSelect }: CollectionTreeProps) {
  const item = (node: TreeNode) => (
    <li key={node.collection.id}>
      <RouterLink href={`/knowledge?${new URLSearchParams({ collection: node.collection.id })}`} className="kb-tree-link" current={selected === node.collection.id}
        label={`${node.collection.name}, ${kbMessages.articleCount(node.collection.article_count)}`} onFollow={() => onSelect(node.collection)}>
        <span className="kb-tree-name">{node.collection.name}</span>
        <span className="kb-tree-count">{node.collection.article_count}</span>
      </RouterLink>
      {node.children.length > 0 && <ul>{node.children.map(item)}</ul>}
    </li>
  );
  return (
    <ul className="kb-tree">
      <li>
        <RouterLink href="/knowledge" className="kb-tree-link" current={!selected} onFollow={() => onSelect(undefined)}>
          <span className="kb-tree-name">{kbMessages.allArticles}</span>
        </RouterLink>
      </li>
      {tree.map(item)}
    </ul>
  );
}
