import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { formatDate } from '../../lib/format-date';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';
import { SearchIcon } from '../../ui/icons';
import { typingDelayMs } from '../inbox/inbox-filters';
import { RouterLink } from '../../ui/router-link';
import { editorPart } from './editor-part';
import { SavedReplyTrash } from '../settings/saved-reply-panels';
import { CollectionForm, CollectionTree } from './collection-panel';
import type { CollectionValues } from './collection-panel';
import { kbApi } from './kb-api';
import type { KBArticleStatus, KBArticleSummary, KBCollection } from './kb-api';
import { kbMessages } from './kb-messages';
import { KBDialog, KBHead } from './kb-ui';
import { Icon } from './editor/icons';
import { buildTree, flatten, indent, maxDepth, reason } from './kb-tree';
import type { TreeNode } from './kb-tree';
import { OrderAnnouncement, OrderButtons, allArticleIds, useOrderMoves } from './order-buttons';
import type { Step } from './order-buttons';

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
  const closeTrash = () => setTrashOpen(false);
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
  // A collection's whole list, unfiltered, is its order: only then can a row move. A row that cannot be
  // shown after its move leaves the focus on the list's heading.
  const focusHeading = () => document.getElementById('kb-articles-heading')?.focus();
  const moves = useOrderMoves(refresh, focusHeading);
  const ordered = selected && !search.q && !search.status ? selected : undefined;
  const articleIds = items.map((article) => article.id);
  const parent = selected ? nodes.find((node) => node.collection.id === selected.collection.parent_id) : undefined;
  const siblings = selected ? parent?.children ?? tree : [];
  const siblingIds = siblings.map((node) => node.collection.id);
  // An order names every article of the parent: the page has a collection's articles only while it is the
  // one shown, and maybe a page of them, so the rest is read first (the top holds none, an empty parent
  // needs no read). A list that went stale meanwhile is completed by the server's answer (sendMove).
  const moveCollection = (node: TreeNode, step: Step) => moves.send(node.collection.parent_id, async () => ({
    collection_ids: siblingIds,
    article_ids: parent && parent.collection.article_count > 0 ? await allArticleIds(parent.collection.id) : [],
  }), 'collection_ids', node.collection.id, step);
  const moveArticle = async (collection: TreeNode, id: string, step: Step) => {
    const loaded = articleIds.length;
    const saved = await moves.send(collection.collection.id, async () => ({
      collection_ids: collection.children.map((node) => node.collection.id),
      article_ids: articles.hasNextPage ? await allArticleIds(collection.collection.id) : articleIds,
    }), 'article_ids', id, step);
    // Moved past the loaded rows: load the next page so the row (and the focus on it) comes back.
    if (saved.indexOf(id) >= loaded) {
      const next = await articles.fetchNextPage();
      if (!next.data?.pages.some((page) => page.items.some((item) => item.id === id))) {
        moves.focus.current = null;
        focusHeading();
      }
    }
    return saved;
  };
  const names = new Map(nodes.map((node) => [node.collection.id, node.collection.name]));
  const noCollections = collections.isSuccess && nodes.length === 0;
  const total = nodes.reduce((sum, node) => sum + node.collection.article_count, 0);
  // Published is not public while the help center is off: the page says so, and how to turn it on.
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter });
  const publicNow = center.data?.enabled;
  const newCollection = () => { setProblem(undefined); setForm(form === 'new' ? null : 'new'); };
  const newArticle = `/knowledge/new${selected ? `?${new URLSearchParams({ collection: selected.collection.id })}` : ''}`;
  return (
    <main className="kb-frame kb-articles-page" aria-labelledby="kb-heading">
      <KBHead current="articles" heading={kbMessages.heading} articles={collections.isSuccess ? total : undefined} actions={<>
        <button type="button" aria-haspopup="dialog" aria-label={kbMessages.trash} onClick={() => setTrashOpen(true)}><Icon name="trash" /><span className="kb-label">{kbMessages.trash}</span></button>
        {/* A pointer or the focus on New article loads the editor's chunk ahead of the click (editor-part.ts). */}
        {!noCollections && <span onPointerOver={editorPart.preload} onFocus={editorPart.preload}><RouterLink className="button-link primary-action" href={newArticle}
          onFollow={() => void navigate({ to: '/knowledge/new', search: selected ? { collection: selected.collection.id } : {} })}><Icon name="plus" />{kbMessages.newArticle}</RouterLink></span>}
      </>} />
      {publicNow === false && <div className="kb-notice" role="status">
        <Icon name="globe" />
        <span><strong>{kbMessages.helpCenterIsOff}</strong> {kbMessages.helpCenterIsOffDetail}</span>
        <RouterLink className="button-link" href="/settings#help-center-heading" onFollow={() => void navigate({ to: '/settings', hash: 'help-center-heading' })}>{kbMessages.turnOnInSettings}</RouterLink>
      </div>}
      {trashOpen && <KBTrash timezone={timezone} onChanged={() => void refresh()} onClose={closeTrash} />}
      <OrderAnnouncement message={moves.message} />
      <div className="kb-art-layout">
        <nav className="kb-collections" aria-labelledby="kb-collections-heading" data-form={form === 'new' || noCollections ? 'true' : undefined}>
          <div className="kb-collections-head">
            <h2 id="kb-collections-heading">{kbMessages.collections}</h2>
            <button type="button" className="icon-button" aria-label={kbMessages.newCollection} data-tip={kbMessages.newCollection} aria-expanded={form === 'new'} onClick={newCollection}><Icon name="plus" /></button>
          </div>
          {collections.isError && <p role="alert" className="error-copy">{kbMessages.collectionsFailed}{' '}
            <button type="button" className="link-action" onClick={() => void collections.refetch()}>{kbMessages.retry}</button></p>}
          {(form === 'new' || (noCollections && form === null)) && <CollectionForm tree={tree} onSubmit={save} onCancel={() => setForm(null)} />}
          {noCollections && <p className="muted-copy">{kbMessages.noCollections}</p>}
          {nodes.length > 0 && <div className="kb-tree-scroll"><CollectionTree tree={tree} total={total} selected={search.collection} onSelect={(collection) => setSearch({ collection: collection?.id })} /></div>}
        </nav>
        <section className="kb-list" aria-labelledby="kb-articles-heading" data-order={ordered ? 'true' : undefined}>
          {selected && <CollectionHead node={selected} tree={tree} form={form} confirming={confirming} problem={problem}
            onForm={(next) => { setProblem(undefined); setConfirming(false); setForm(next); }} onSave={save}
            onConfirm={setConfirming} onDelete={() => void remove(selected.collection)}
            order={siblingIds.length > 1 && <OrderButtons text ids={siblingIds} id={selected.collection.id} name={selected.collection.name} busy={moves.moving} onAnnounce={moves.announce}
              focus={moves.focus} onMove={(_next, step) => moveCollection(selected, step)} />} />}
          {!selected && <h2 id="kb-articles-heading" className="sr-only">{kbMessages.allArticles}</h2>}
          <div className="kb-list-tools" role="search">
            {nodes.length > 0 && <span className="kb-phone-collections">
              <select aria-label={kbMessages.collection} value={search.collection ?? ''} onChange={(event) => setSearch({ collection: event.target.value || undefined })}>
                <option value="">{`${kbMessages.allArticles} (${total})`}</option>
                {nodes.map((node) => <option key={node.collection.id} value={node.collection.id}>{`${indent(node)} (${node.collection.article_count})`}</option>)}
              </select>
              <button type="button" className="icon-button" aria-label={kbMessages.newCollection} aria-expanded={form === 'new'} onClick={newCollection}><Icon name="plus" /></button>
            </span>}
            <label className="search-field kb-search">
              <SearchIcon />
              <span className="sr-only">{kbMessages.searchArticles}</span>
              <input type="search" value={text} placeholder={kbMessages.searchPlaceholder} maxLength={200} aria-describedby="kb-search-hint"
                onChange={(event) => type(event.target.value)} />
            </label>
            <span id="kb-search-hint" className="sr-only">{kbMessages.searchHint}</span>
            <fieldset className="segmented">
              <legend className="sr-only">{kbMessages.statusFilter}</legend>
              {([[undefined, kbMessages.anyStatus], ['published', kbMessages.publishedFilter], ['draft', kbMessages.drafts]] as const).map(([value, label]) => (
                <label key={label}><input type="radio" name="kb-status" checked={search.status === value} onChange={() => setSearch({ status: value })} />{label}</label>
              ))}
            </fieldset>
          </div>
          <div className="kb-cols-head" aria-hidden="true"><span>{kbMessages.articleColumn}</span><span>{kbMessages.statusFilter}</span><span>{kbMessages.updatedColumn}</span><span />{ordered && <span />}</div>
          <div className="kb-list-scroll">
            {articles.isError && <p role="alert" className="error-copy kb-list-note">{kbMessages.listFailed}{' '}
              <button type="button" className="link-action" onClick={() => void articles.refetch()}>{kbMessages.retry}</button></p>}
            {articles.isPending && <p role="status" className="empty-copy kb-list-note">{messages.loading}</p>}
            {articles.isSuccess && items.length === 0 && <p className="empty-copy kb-list-note">{search.q ? kbMessages.noMatches : kbMessages.noArticles}</p>}
            {items.length > 0 && <ul className="kb-article-list" aria-busy={articles.isFetching && !articles.isFetchingNextPage}
              onPointerOver={editorPart.preload} onFocus={editorPart.preload}>
              {items.map((article) => <ArticleRow key={article.id} article={article} timezone={timezone} isPublic={publicNow}
                collection={names.get(article.collection_id)}
                order={ordered && <OrderButtons ids={articleIds} id={article.id} name={article.title} more={articles.hasNextPage} busy={moves.moving} onAnnounce={moves.announce}
                  focus={moves.focus} onMove={(_next, step) => moveArticle(ordered, article.id, step)} />}
                onOpen={() => void navigate({ to: '/knowledge/articles/$articleId', params: { articleId: article.id } })} />)}
              {articles.hasNextPage && <li className="kb-more-row"><button type="button" className="kb-more" disabled={articles.isFetchingNextPage} onClick={() => void articles.fetchNextPage()}>
                {articles.isFetchingNextPage ? messages.loading : kbMessages.loadMore}</button></li>}
            </ul>}
          </div>
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
  // order holds the buttons that move the collection among the ones beside it.
  order?: ReactNode;
}

// CollectionHead names the chosen collection, with its description and what can be done to it.
function CollectionHead({ node, tree, form, confirming, problem, onForm, onSave, onConfirm, onDelete, order }: CollectionHeadProps) {
  const { collection } = node;
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
            <button type="button" className="danger-action" aria-label={kbMessages.deleteCollectionName(collection.name)} onClick={() => onConfirm(true)}>{kbMessages.deleteCollection}</button>
            {order}
          </div>}
      {form === 'edit' && <CollectionForm key={collection.updated_at} tree={tree} node={node} onSubmit={onSave} onCancel={() => onForm(null)} />}
      {form === 'new-inside' && <CollectionForm tree={tree} parentId={collection.id} onSubmit={onSave} onCancel={() => onForm(null)} />}
    </div>
  );
}

interface ArticleRowProps {
  article: KBArticleSummary;
  collection?: string;
  timezone: string;
  isPublic?: boolean;
  // order holds the buttons that move the article in its collection, while the list is the whole order.
  order?: ReactNode;
  onOpen: () => void;
}

function ArticleRow({ article, collection, timezone, isPublic, order, onOpen }: ArticleRowProps) {
  const change = article.last_change;
  const published = article.status === 'published';
  // Readers can open it only while it is published and the help center is on.
  const open = published && isPublic === true;
  return (
    <li className="kb-article-item">
      <RouterLink className="kb-article-row" href={`/knowledge/articles/${article.id}`} onFollow={onOpen}>
        <span className="kb-article-main">
          <span className="kb-article-title">{article.title}</span>
          <span className="kb-article-meta">{[collection, kbMessages.articleNumber(article.number)].filter(Boolean).join(' · ')}</span>
        </span>
        <span className="kb-article-status">
          <span className="kb-dot-status" data-status={article.status}>{published ? kbMessages.published : kbMessages.draft}</span>
          <small>{published ? (isPublic === false ? kbMessages.notPublic : isPublic ? kbMessages.public : '') : kbMessages.agentsOnly}</small>
        </span>
        <span className="kb-article-when"><time dateTime={article.updated_at}>{formatDate(article.updated_at, timezone)}</time>
          {change && <small>{savedReplyChangeText(change)}</small>}</span>
      </RouterLink>
      {open
        ? <a className="kb-article-open icon-button" href={article.url} target="_blank" rel="noreferrer" aria-label={kbMessages.openPublic(article.title)} data-tip={kbMessages.openOnHelpCenter}><Icon name="external" /></a>
        : <span className="kb-article-open" aria-hidden="true" title={published ? kbMessages.turnOnToOpen : undefined}><Icon name="external" /></span>}
      {order}
    </li>
  );
}

// KBTrash holds the deleted articles and collections, each restorable for 30 days.
function KBTrash({ timezone, onChanged, onClose }: { timezone: string; onChanged: () => void; onClose: () => void }) {
  const [now] = useState(() => new Date());
  return (
    <KBDialog title={kbMessages.trash} className="kb-trash" phone="full" closeLabel={kbMessages.closeTrash} onClose={onClose}>
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.articleTrash} description={kbMessages.articleTrashDescription}
        empty={kbMessages.articleTrashEmpty} failedMessage={kbMessages.articleActionFailed} text={() => ''}
        load={(cursor) => kbApi.trashedArticles(cursor)}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreArticle(id, updatedAt); onChanged(); }} />
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.collectionTrash} description={kbMessages.collectionTrashDescription}
        empty={kbMessages.collectionTrashEmpty} failedMessage={kbMessages.collectionActionFailed} text={(item) => item.description}
        load={async () => ({ items: (await kbApi.trashedCollections()).items.map((item) => ({ ...item, title: item.name })) })}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreCollection(id, updatedAt); onChanged(); }} />
    </KBDialog>
  );
}
