import { useState } from 'react';
import type { ReactNode } from 'react';
import { CollectionForm } from './collection-panel';
import type { CollectionValues } from './collection-panel';
import { CollectionNames } from './collection-names';
import { kbMessages } from './kb-messages';
import { useRefreshKB } from './kb-refresh';
import { maxDepth } from './kb-tree';
import type { TreeNode } from './kb-tree';
import { translationMessages } from './translation-messages';

interface CollectionHeadProps {
  node: TreeNode;
  tree: TreeNode[];
  form: 'new' | 'new-inside' | 'edit' | null;
  confirming: boolean;
  problem?: string;
  onForm: (form: 'new-inside' | 'edit' | null) => void;
  onSave: (values: CollectionValues) => Promise<unknown>;
  onConfirm: (confirming: boolean) => void;
  onDelete: () => void;
  // order holds the buttons that move the collection among the ones beside it.
  order?: ReactNode;
}

// CollectionHead names the chosen collection, with its description and what can be done to it.
export function CollectionHead({ node, tree, form, confirming, problem, onForm, onSave, onConfirm, onDelete, order }: CollectionHeadProps) {
  const { collection } = node;
  const [naming, setNaming] = useState(false);
  const refresh = useRefreshKB();
  return (
    <div className="kb-collection-head">
      <div className="kb-collection-title">
        <h2 id="kb-articles-heading" tabIndex={-1}>{collection.name}</h2>
        {collection.description && <p className="muted-copy">{collection.description}</p>}
      </div>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {confirming
        ? <div role="group" aria-label={kbMessages.deleteCollectionName(collection.name)} className="kb-confirm">
            <span>{kbMessages.confirmDeleteCollection(collection.name)}</span>
            <div className="button-row">
              <button type="button" className="danger-action" onClick={onDelete}>{kbMessages.confirmDelete}</button>
              <button type="button" onClick={() => onConfirm(false)}>{kbMessages.cancel}</button>
            </div>
          </div>
        : form === null && <div className="button-row">
            <button type="button" aria-label={kbMessages.editCollectionName(collection.name)} onClick={() => onForm('edit')}>{kbMessages.editCollection}</button>
            {node.depth < maxDepth
              ? <button type="button" aria-label={kbMessages.newSubcollectionOf(collection.name)} onClick={() => onForm('new-inside')}>{kbMessages.newSubcollection}</button>
              : <span className="kb-hint">{kbMessages.maxDepth}</span>}
            <button type="button" aria-label={translationMessages.collectionNamesOf(collection.name)} onClick={() => setNaming(true)}>{translationMessages.namesInLanguages}</button>
            <button type="button" className="danger-action" aria-label={kbMessages.deleteCollectionName(collection.name)} onClick={() => onConfirm(true)}>{kbMessages.deleteCollection}</button>
            {order}
          </div>}
      {form === 'edit' && <CollectionForm key={collection.updated_at} tree={tree} node={node} onSubmit={onSave} onCancel={() => onForm(null)} />}
      {form === 'new-inside' && <CollectionForm tree={tree} parentId={collection.id} onSubmit={onSave} onCancel={() => onForm(null)} />}
      {naming && <CollectionNames collection={collection} onChanged={refresh} onClose={() => setNaming(false)} />}
    </div>
  );
}
