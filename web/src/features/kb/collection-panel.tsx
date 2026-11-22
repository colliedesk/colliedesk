import { useId, useState } from 'react';
import type { FormEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useHolds } from '../../lib/permissions-context';
import { HelpTip } from '../../ui/help-tip';
import { CollectionApprovalField } from './collection-approval-field';
import { CollectionOrderField } from './collection-order-field';
import { CollectionVisibilityField } from './collection-visibility-field';
import type { KBCollection, KBVisibility } from './kb-api';
import { kbMessages } from './kb-messages';
import { kbTemplatesApi, templatesKey } from './kb-templates-api';
import { templateMessages } from './template-messages';
import { indent, parentChoices, reason } from './kb-tree';
import type { TreeNode } from './kb-tree';

export interface CollectionValues {
  name: string;
  slug: string;
  description: string;
  parent_id: string | null;
  // default_template_id is the template every new article in it starts from; null starts them blank.
  default_template_id: string | null;
  // require_approval overrides the help center's approval before publishing for its articles.
  require_approval: KBCollection['require_approval'];
  // article_order is how it lists its articles; the order set by hand stays stored under every other.
  article_order: KBCollection['article_order'];
  // visibility narrows who can see every article in it and its collections.
  visibility: KBVisibility;
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
    default_template_id: original?.default_template_id ?? null, require_approval: original?.require_approval ?? 'inherit',
    article_order: original?.article_order ?? 'manual', visibility: original?.visibility ?? 'everyone',
  });
  const holds = useHolds();
  const templates = useQuery({ queryKey: templatesKey, queryFn: kbTemplatesApi.list }).data?.items ?? [];
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
        <input id={`${id}-slug-input`} value={values.slug} aria-describedby={`${id}-slug ${id}-slug-tip`} onChange={(event) => set({ slug: event.target.value })} />
        <small className="muted-copy"><span id={`${id}-slug`}>{kbMessages.collectionSlugHelp}</span>{' '}
          <HelpTip label={kbMessages.collectionSlug} text={kbMessages.collectionSlugTip} id={`${id}-slug-tip`} /></small></div>}
      {slugChanged && <p className="warning-note" role="note">{kbMessages.slugChangeWarning}</p>}
      <label className="field"><span>{kbMessages.collectionDescription}</span>
        <textarea rows={2} value={values.description} onChange={(event) => set({ description: event.target.value })} /></label>
      {/* With no collection to go inside, Top level is the only place: nothing to ask. */}
      {choices.length > 0 && <label className="field"><span>{kbMessages.collectionParent}</span>
        <select value={values.parent_id ?? ''} onChange={(event) => set({ parent_id: event.target.value || null })}>
          <option value="">{kbMessages.topLevel}</option>
          {choices.map((choice) => <option key={choice.collection.id} value={choice.collection.id}>{indent(choice)}</option>)}
        </select></label>}
      {/* With no template there is nothing to start from: every article starts blank. */}
      {templates.length > 0 && <div className="field"><label htmlFor={`${id}-template`}>{templateMessages.defaultTemplate}</label>
        <select id={`${id}-template`} value={values.default_template_id ?? ''} aria-describedby={`${id}-template-note ${id}-template-tip`} onChange={(event) => set({ default_template_id: event.target.value || null })}>
          <option value="">{templateMessages.noDefault}</option>
          {templates.map((template) => <option key={template.id} value={template.id}>{template.name}</option>)}
        </select>
        <small className="muted-copy"><span id={`${id}-template-note`}>{templateMessages.defaultTemplateNote}</span>{' '}
          <HelpTip label={templateMessages.defaultTemplate} text={templateMessages.defaultTemplateTip} id={`${id}-template-tip`} /></small></div>}
      {original && <CollectionOrderField value={values.article_order} onChange={(order) => set({ article_order: order })} />}
      <CollectionVisibilityField value={values.visibility} onChange={(visibility) => set({ visibility })} />
      {/* Changing it is a help center setting: the server asks for help_center:update too. */}
      {original && holds('help_center:update') && <CollectionApprovalField value={values.require_approval} onChange={(mode) => set({ require_approval: mode })} />}
      <div className="button-row">
        <button className="primary-action" type="submit" disabled={busy || !values.name.trim()}>{original ? kbMessages.saveCollection : kbMessages.createCollection}</button>
        <button type="button" disabled={busy} onClick={onCancel}>{kbMessages.cancel}</button>
      </div>
    </form>
  );
}

interface CollectionTreeProps {
  tree: TreeNode[];
  // total counts the articles of every collection, for All articles.
  total?: number;
  selected?: string;
  // hrefTo is the address of the article list with that collection chosen (none: all articles).
  hrefTo: (collection?: KBCollection) => string;
}

// CollectionTree lists the collections as nested links; the chosen one filters the articles.
export function CollectionTree({ tree, total, selected, hrefTo }: CollectionTreeProps) {
  const item = (node: TreeNode) => (
    <li key={node.collection.id}>
      <a href={hrefTo(node.collection)} className="kb-tree-link" aria-current={selected === node.collection.id ? 'page' : undefined}
        aria-label={`${node.collection.name}, ${kbMessages.articleCount(node.collection.article_count)}`}>
        <span className="kb-tree-name">{node.collection.name}</span>
        <span className="kb-tree-count">{node.collection.article_count}</span>
      </a>
      {node.children.length > 0 && <ul>{node.children.map(item)}</ul>}
    </li>
  );
  return (
    <ul className="kb-tree">
      <li>
        <a href={hrefTo()} className="kb-tree-link" aria-current={selected ? undefined : 'page'}>
          <span className="kb-tree-name">{kbMessages.allArticles}</span>
          {total !== undefined && <span className="kb-tree-count" aria-hidden="true">{total}</span>}
        </a>
      </li>
      {tree.map(item)}
    </ul>
  );
}
