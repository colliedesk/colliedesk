import { useEffect, useMemo, useRef, useState } from 'react';
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { formatDate } from '../../lib/format-date';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';
import { SearchIcon } from '../../ui/icons';
import { typingDelayMs } from '../inbox/inbox-filters';
import { RouterLink } from '../../ui/router-link';
import { SavedReplyTrash } from '../settings/saved-reply-panels';
import { CollectionForm, CollectionTree } from './collection-panel';
import type { CollectionValues } from './collection-panel';
import { kbApi } from './kb-api';
import type { KBArticleStatus, KBArticleSummary, KBCollection } from './kb-api';
import { kbMessages } from './kb-messages';
import { buildTree, flatten, maxDepth, reason } from './kb-tree';
import type { TreeNode } from './kb-tree';

export interface KnowledgeSearch {
  collection?: string;
  q?: string;
  status?: KBArticleStatus;
}


// Every write can change the counts in the tree, the list and the trash, so all three are read again.
export function useRefreshKB() {
  const client = useQueryClient();
  return () => Promise.all([client.invalidateQueries({ queryKey: ['kb-collections'] }), client.invalidateQueries({ queryKey: ['kb-articles'] }),
    client.invalidateQueries({ queryKey: ['kb-trash'] })]);
}

export function KnowledgeRoute({ search }: { search: KnowledgeSearch }) {
  const navigate = useNavigate();
  const refresh = useRefreshKB();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timezone = me.data?.timezone ?? 'UTC';
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections });
  const tree = useMemo(() => buildTree(collections.data?.items ?? []), [collections.data]);
  const nodes = useMemo(() => flatten(tree), [tree]);
  const selected = search.collection ? nodes.find((node) => node.collection.id === search.collection) : undefined;
  const filter = { q: search.q, status: search.status, collection_id: search.collection };
  const articles = useInfiniteQuery({
    queryKey: ['kb-articles', filter],
    queryFn: ({ pageParam, signal }) => kbApi.articles({ ...filter, cursor: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  });
  // The latest filters, so a search typed before a status was picked does not bring the old status back.
  const current = useRef(search);
  current.current = search;
  const setSearch = (change: Partial<KnowledgeSearch>, replace = false) =>
    void navigate({ to: '/knowledge', search: { ...current.current, ...change }, replace });
  // The search box writes the address as the person types, a moment after the last key.
  const [text, setText] = useState(search.q ?? '');
  useEffect(() => setText(search.q ?? ''), [search.q]);
  const typed = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(typed.current), []);
  const type = (value: string) => {
    setText(value);
    window.clearTimeout(typed.current);
    typed.current = window.setTimeout(() => setSearch({ q: value.trim() || undefined }, true), typingDelayMs);
  };
  // One form at a time: a new collection (inside the chosen one, or at the top), or editing the chosen one.
  const [form, setForm] = useState<'new' | 'new-inside' | 'edit' | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [trashOpen, setTrashOpen] = useState(false);
  const trashButton = useRef<HTMLButtonElement>(null);
  const closeTrash = () => { setTrashOpen(false); trashButton.current?.focus(); };
  useEffect(() => { setForm(null); setConfirming(false); setProblem(undefined); }, [search.collection]);
  const save = async (values: CollectionValues) => {
    if (form === 'edit' && selected) {
      const original = selected.collection;
      await kbApi.updateCollection(original.id, {
        updated_at: original.updated_at,
        ...(values.name !== original.name ? { name: values.name } : {}),
        ...(values.slug && values.slug !== original.slug ? { slug: values.slug } : {}),
        ...(values.description !== original.description ? { description: values.description } : {}),
        ...(values.parent_id !== original.parent_id ? { parent_id: values.parent_id } : {}),
      });
      await refresh();
      setForm(null);
      return;
    }
    const created = await kbApi.createCollection({ name: values.name, ...(values.description ? { description: values.description } : {}), parent_id: values.parent_id });
    await refresh();
    setForm(null);
    setSearch({ collection: created.id });
  };
  const remove = async (collection: KBCollection) => {
    setProblem(undefined);
    try {
      await kbApi.deleteCollection(collection.id, collection.updated_at);
      await refresh();
      setSearch({ collection: undefined });
    } catch (error) {
      setProblem(reason(error, kbMessages.collectionActionFailed));
    } finally {
      setConfirming(false);
    }
  };
  const items = articles.data?.pages.flatMap((page) => page.items) ?? [];
  const names = new Map(nodes.map((node) => [node.collection.id, node.collection.name]));
  const noCollections = collections.isSuccess && nodes.length === 0;
  return (
    <main className="settings-page kb-page" aria-labelledby="kb-heading">
      <header className="kb-header">
        <div>
          <h1 id="kb-heading">{kbMessages.heading}</h1>
          <p>{kbMessages.description}</p>
        </div>
        <div className="button-row">
          <button type="button" ref={trashButton} aria-expanded={trashOpen} onClick={() => (trashOpen ? closeTrash() : setTrashOpen(true))}>{kbMessages.trash}</button>
          {!noCollections && <RouterLink className="button-link primary-action" href={`/knowledge/new${selected ? `?${new URLSearchParams({ collection: selected.collection.id })}` : ''}`}
            onFollow={() => void navigate({ to: '/knowledge/new', search: selected ? { collection: selected.collection.id } : {} })}>{kbMessages.newArticle}</RouterLink>}
        </div>
      </header>
      {trashOpen && <KBTrash timezone={timezone} onChanged={() => void refresh()} onClose={closeTrash} />}
      <div className="kb-layout">
        <nav className="kb-collections" aria-labelledby="kb-collections-heading">
          <div className="kb-collections-head">
            <h2 id="kb-collections-heading">{kbMessages.collections}</h2>
            <button type="button" aria-expanded={form === 'new'} onClick={() => { setProblem(undefined); setForm(form === 'new' ? null : 'new'); }}>{kbMessages.newCollection}</button>
          </div>
          {collections.isError && <p role="alert" className="error-copy">{kbMessages.collectionsFailed}{' '}
            <button type="button" className="link-action" onClick={() => void collections.refetch()}>{kbMessages.retry}</button></p>}
          {(form === 'new' || (noCollections && form === null)) && <CollectionForm tree={tree} onSubmit={save} onCancel={() => setForm(null)} />}
          {noCollections && <p className="muted-copy">{kbMessages.noCollections}</p>}
          {nodes.length > 0 && <CollectionTree tree={tree} selected={search.collection} onSelect={(collection) => setSearch({ collection: collection?.id })} />}
        </nav>
        <section className="kb-articles" aria-labelledby="kb-articles-heading">
          {selected && <CollectionHead node={selected} tree={tree} form={form} confirming={confirming} problem={problem}
            onForm={(next) => { setProblem(undefined); setConfirming(false); setForm(next); }} onSave={save}
            onConfirm={setConfirming} onDelete={() => void remove(selected.collection)} />}
          {!selected && <h2 id="kb-articles-heading" className="kb-articles-title">{kbMessages.allArticles}</h2>}
          <div className="kb-filters" role="search">
            <label className="search-field kb-search">
              <SearchIcon />
              <span className="sr-only">{kbMessages.searchArticles}</span>
              <input type="search" value={text} placeholder={kbMessages.searchArticles} maxLength={200} aria-describedby="kb-search-hint"
                onChange={(event) => type(event.target.value)} />
            </label>
            <fieldset className="segmented">
              <legend className="sr-only">{kbMessages.statusFilter}</legend>
              {([[undefined, kbMessages.anyStatus], ['draft', kbMessages.draft], ['published', kbMessages.published]] as const).map(([value, label]) => (
                <label key={label}><input type="radio" name="kb-status" checked={search.status === value} onChange={() => setSearch({ status: value })} />{label}</label>
              ))}
            </fieldset>
          </div>
          <p id="kb-search-hint" className="kb-hint">{kbMessages.searchHint}</p>
          {articles.isError && <p role="alert" className="error-copy">{kbMessages.listFailed}{' '}
            <button type="button" className="link-action" onClick={() => void articles.refetch()}>{kbMessages.retry}</button></p>}
          {articles.isPending && <p role="status" className="empty-copy">{messages.loading}</p>}
          {articles.isSuccess && items.length === 0 && <p className="empty-copy">{search.q ? kbMessages.noMatches : kbMessages.noArticles}</p>}
          {items.length > 0 && <ul className="kb-article-list" aria-busy={articles.isFetching && !articles.isFetchingNextPage}>
            {items.map((article) => <ArticleRow key={article.id} article={article} timezone={timezone}
              collection={selected ? undefined : names.get(article.collection_id)}
              onOpen={() => void navigate({ to: '/knowledge/articles/$articleId', params: { articleId: article.id } })} />)}
          </ul>}
          {articles.hasNextPage && <button type="button" className="kb-more" disabled={articles.isFetchingNextPage} onClick={() => void articles.fetchNextPage()}>
            {articles.isFetchingNextPage ? messages.loading : kbMessages.loadMore}</button>}
        </section>
      </div>
    </main>
  );
}

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
}

// CollectionHead names the chosen collection, with its description and what can be done to it.
function CollectionHead({ node, tree, form, confirming, problem, onForm, onSave, onConfirm, onDelete }: CollectionHeadProps) {
  const { collection } = node;
  return (
    <div className="kb-collection-head">
      <div className="kb-collection-title">
        <h2 id="kb-articles-heading">{collection.name}</h2>
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
            <button type="button" className="danger-action" aria-label={kbMessages.deleteCollectionName(collection.name)} onClick={() => onConfirm(true)}>{kbMessages.deleteCollection}</button>
          </div>}
      {form === 'edit' && <CollectionForm key={collection.updated_at} tree={tree} node={node} onSubmit={onSave} onCancel={() => onForm(null)} />}
      {form === 'new-inside' && <CollectionForm tree={tree} parentId={collection.id} onSubmit={onSave} onCancel={() => onForm(null)} />}
    </div>
  );
}

function ArticleRow({ article, collection, timezone, onOpen }: { article: KBArticleSummary; collection?: string; timezone: string; onOpen: () => void }) {
  const change = article.last_change;
  return (
    <li>
      <RouterLink className="kb-article-row" href={`/knowledge/articles/${article.id}`} onFollow={onOpen}>
        <span className="kb-article-title">{article.title}</span>
        <span className="kb-article-meta">
          <span className="badge" data-status={article.status}>{article.status === 'published' ? kbMessages.published : kbMessages.draft}</span>
          <span>{kbMessages.articleNumber(article.number)}</span>
          {collection && <span>{kbMessages.inCollection(collection)}</span>}
          <span>{change ? `${savedReplyChangeText(change)} · ` : ''}<time dateTime={article.updated_at}>{formatDate(article.updated_at, timezone)}</time></span>
        </span>
      </RouterLink>
    </li>
  );
}

// KBTrash holds the deleted articles and collections, each restorable for 30 days.
function KBTrash({ timezone, onChanged, onClose }: { timezone: string; onChanged: () => void; onClose: () => void }) {
  const [now] = useState(() => new Date());
  return (
    <section className="kb-trash" aria-label={kbMessages.trash}>
      <div className="saved-reply-panel-head">
        <h2>{kbMessages.trash}</h2>
        <button type="button" onClick={onClose}>{kbMessages.closeTrash}</button>
      </div>
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.articleTrash} description={kbMessages.articleTrashDescription}
        empty={kbMessages.articleTrashEmpty} failedMessage={kbMessages.articleActionFailed} text={() => ''}
        load={(cursor) => kbApi.trashedArticles(cursor)}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreArticle(id, updatedAt); onChanged(); }} />
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.collectionTrash} description={kbMessages.collectionTrashDescription}
        empty={kbMessages.collectionTrashEmpty} failedMessage={kbMessages.collectionActionFailed} text={(item) => item.description}
        load={async () => ({ items: (await kbApi.trashedCollections()).items.map((item) => ({ ...item, title: item.name })) })}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreCollection(id, updatedAt); onChanged(); }} />
    </section>
  );
}
