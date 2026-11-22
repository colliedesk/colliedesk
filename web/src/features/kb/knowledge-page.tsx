import { useEffect, useMemo, useRef, useState } from 'react';
import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { KBListColumn } from '../../api/schemas';
import { messages } from '../../messages';
import { SearchIcon } from '../../ui/icons';
import { typingDelayMs } from '../inbox/inbox-filters';
import { usePreloadEditor } from './editor-part';
import { ArticleRow } from './article-row';
import { CollectionForm, CollectionTree } from './collection-panel';
import type { CollectionValues } from './collection-panel';
import { CollectionHead } from './collection-head';
import { kbApi } from './kb-api';
import type { KBCollection } from './kb-api';
import { useRefreshKB } from './kb-refresh';
import { KBSavedViews } from './kb-saved-views';
import { KBTrash } from './kb-trash';
import { VisibilityFilter } from './visibility-badge';
import { kbMessages } from './kb-messages';
import { articleFiltersOf, collectionHref, filtersBeyondCollection } from './knowledge-search';
import type { KnowledgeSearch } from './knowledge-search';
import { ListColumns, gridOf } from './list-columns';
import { ListFilters } from './list-filters';
import { listMessages } from './list-messages';
import { NewArticleMenu } from './new-article-menu';
import { HelpTip } from '../../ui/help-tip';
import { Icon } from './editor/icons';
import { buildTree, flatten, indent, reason } from './kb-tree';
import type { TreeNode } from './kb-tree';
import { OrderAnnouncement, OrderButtons, allArticleIds, useOrderMoves } from './order-buttons';
import type { Step } from './order-buttons';
import { useCreateAction } from '../../api/use-create';
import { useHolds } from '../../lib/permissions-context';
import { KBBulkBar } from './kb-bulk-bar';
import { kbBulkMessages } from './kb-bulk-messages';
import { useKBBulk } from './use-kb-bulk';
import { KBHead, KBToast } from './kb-ui';

export type { KnowledgeSearch } from './knowledge-search';
export { useRefreshKB } from './kb-refresh';

const defaultColumns: KBListColumn[] = ['status', 'author', 'tags', 'updated'];
// The head of a column: status and updated keep the words the list always had.
const columnHead = (column: KBListColumn) => (column === 'status' ? kbMessages.statusFilter : column === 'updated' ? kbMessages.updatedColumn : listMessages.columnNames[column]);

// The status filter: each choice sets both status and has_draft, so one choice never keeps the other's.
const statusChoices = [[{ status: undefined, has_draft: undefined }, kbMessages.anyStatus], [{ status: 'published', has_draft: undefined }, kbMessages.publishedFilter],
  [{ status: 'draft', has_draft: undefined }, kbMessages.drafts], [{ status: undefined, has_draft: 'true' }, kbMessages.unpublishedChanges]] as const;


export function KnowledgeRoute({ search }: { search: KnowledgeSearch }) {
  const navigate = useNavigate();
  const refresh = useRefreshKB();
  const preloadEditor = usePreloadEditor();
  const createCollection = useCreateAction((value: Parameters<typeof kbApi.createCollection>[0], key: string) => kbApi.createCollection(value, key));
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timezone = me.data?.timezone ?? 'UTC';
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections });
  const tree = useMemo(() => buildTree(collections.data?.items ?? []), [collections.data]);
  const nodes = useMemo(() => flatten(tree), [tree]);
  const selected = search.collection ? nodes.find((node) => node.collection.id === search.collection) : undefined;
  const filter = articleFiltersOf(search);
  const columns = me.data?.kb_list_columns.columns ?? defaultColumns;
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
        ...(values.default_template_id !== (original.default_template_id ?? null) ? { default_template_id: values.default_template_id } : {}),
        ...(values.require_approval !== original.require_approval ? { require_approval: values.require_approval } : {}),
        ...(values.article_order !== original.article_order ? { article_order: values.article_order } : {}),
        ...(values.visibility !== original.visibility ? { visibility: values.visibility } : {}),
      });
      await refresh();
      setForm(null);
      return;
    }
    const created = await createCollection.run({ name: values.name, ...(values.description ? { description: values.description } : {}), parent_id: values.parent_id,
      ...(values.default_template_id ? { default_template_id: values.default_template_id } : {}),
      ...(values.visibility === 'everyone' ? {} : { visibility: values.visibility }) });
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
  // A collection sorted by title or date orders itself: nothing to move by hand.
  const sortedBy = selected && selected.collection.article_order !== 'manual' ? selected.collection.article_order : undefined;
  const ordered = selected && !sortedBy && !filtersBeyondCollection(search) ? selected : undefined;
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
  const holds = useHolds();
  const creates = holds('kb_articles:create');
  const bulk = useKBBulk(filter, refresh);
  const selects = holds('kb_articles:update');
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter, enabled: holds('help_center:read') });
  const publicNow = center.data?.enabled;
  const newCollection = () => { setProblem(undefined); setForm(form === 'new' ? null : 'new'); };
  return (
    <main className="kb-frame kb-articles-page" aria-labelledby="kb-heading">
      <KBHead current="articles" heading={kbMessages.heading} articles={collections.isSuccess ? total : undefined} actions={<>
        <button type="button" aria-haspopup="dialog" aria-label={kbMessages.trash} onClick={() => setTrashOpen(true)}><Icon name="trash" /><span className="kb-label">{kbMessages.trash}</span></button>
        {creates && !noCollections && <NewArticleMenu collectionId={selected?.collection.id} defaultTemplateId={selected?.collection.default_template_id} />}
      </>} />
      {publicNow === false && <div className="kb-notice" role="status">
        <Icon name="globe" />
        <span><strong>{kbMessages.helpCenterIsOff}</strong> {kbMessages.helpCenterIsOffDetail}</span>
        {holds('help_center:update') && <a className="button-link" href="/knowledge/settings#help-center-heading">{kbMessages.turnOnInSettings}</a>}
      </div>}
      {trashOpen && <KBTrash timezone={timezone} onChanged={() => void refresh()} onClose={closeTrash} />}
      <OrderAnnouncement message={moves.message} />
      <div className="kb-art-layout">
        <nav className="kb-collections" aria-labelledby="kb-collections-heading" data-form={creates && (form === 'new' || noCollections) ? 'true' : undefined}>
          <div className="kb-collections-head">
            <h2 id="kb-collections-heading">{kbMessages.collections}</h2>
            {creates && <button type="button" className="icon-button" aria-label={kbMessages.newCollection} data-tip={kbMessages.newCollection} aria-expanded={form === 'new'} onClick={newCollection}><Icon name="plus" /></button>}
          </div>
          {collections.isError && <p role="alert" className="error-copy">{kbMessages.collectionsFailed}{' '}
            <button type="button" className="link-action" onClick={() => void collections.refetch()}>{kbMessages.retry}</button></p>}
          {creates && (form === 'new' || (noCollections && form === null)) && <CollectionForm tree={tree} onSubmit={save} onCancel={() => setForm(null)} />}
          {noCollections && <p className="muted-copy">{creates ? <>{kbMessages.noCollections} <HelpTip label={kbMessages.collections} text={kbMessages.noCollectionsTip} /></> : kbMessages.noCollectionsYet}</p>}
          {nodes.length > 0 && <div className="kb-tree-scroll"><CollectionTree tree={tree} total={total} selected={search.collection} hrefTo={(collection?: KBCollection) => collectionHref(search, collection?.id)} /></div>}
        </nav>
        <section className="kb-list" aria-labelledby="kb-articles-heading" data-order={ordered ? 'true' : undefined} data-select={selects ? 'true' : undefined} style={gridOf(columns, Boolean(ordered))}>
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
              {creates && <button type="button" className="icon-button" aria-label={kbMessages.newCollection} aria-expanded={form === 'new'} onClick={newCollection}><Icon name="plus" /></button>}
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
              {statusChoices.map(([choice, label]) => (
                <label key={label}><input type="radio" name="kb-status" checked={search.status === choice.status && search.has_draft === choice.has_draft}
                  onChange={() => setSearch(choice)} />{label}</label>
              ))}
            </fieldset>
            <VisibilityFilter value={search.visibility} onChange={(visibility) => setSearch({ visibility })} />
            <ListFilters search={search} onChange={(change) => setSearch(change)} />
            <ListColumns columns={columns} isDefault={me.data?.kb_list_columns.default ?? true} />
            <KBSavedViews search={search} timezone={timezone} onApply={(next) => void navigate({ to: '/knowledge', search: next, replace: false })} />
            <a className="button-link kb-export" href={kbApi.exportHref(filter)} download data-tip={listMessages.exportTip}>{listMessages.exportCsv}</a>
          </div>
          {(bulk.count > 0 || bulk.all) && <KBBulkBar count={bulk.count} all={bulk.all} nodes={nodes} meId={me.data?.agent.id} busy={bulk.busy}
            onSelectAll={Object.keys(filter).length > 0 ? bulk.selectAll : undefined} onApply={(change) => void bulk.run(change)} onClear={bulk.clear} />}
          <div className="kb-cols-head" aria-hidden="true"><span>{kbMessages.articleColumn}</span>
            {columns.map((column, index) => <span key={column} data-extra={index > 0 ? 'true' : undefined}>{columnHead(column)}</span>)}<span />{ordered && <span />}</div>
          <div className="kb-list-scroll">
            {articles.isError && <p role="alert" className="error-copy kb-list-note">{kbMessages.listFailed}{' '}
              <button type="button" className="link-action" onClick={() => void articles.refetch()}>{kbMessages.retry}</button></p>}
            {sortedBy && <p className="muted-copy kb-list-note">{kbMessages.sortedBy[sortedBy]} {kbMessages.orderByHand}</p>}
            {articles.isPending && <p role="status" className="empty-copy kb-list-note">{messages.loading}</p>}
            {articles.isSuccess && items.length === 0 && <p className="empty-copy kb-list-note">{search.q ? kbMessages.noMatches : kbMessages.noArticles}</p>}
            {items.length > 0 && <ul className="kb-article-list" aria-busy={articles.isFetching && !articles.isFetchingNextPage}
              onPointerOver={preloadEditor} onFocus={preloadEditor}>
              {items.map((article) => <ArticleRow key={article.id} article={article} columns={columns} timezone={timezone} isPublic={publicNow}
                collection={names.get(article.collection_id)}
                select={selects ? { checked: bulk.all || bulk.ids.includes(article.id), onChange: () => bulk.toggle(article.id, articleIds) } : undefined}
                order={ordered && <OrderButtons ids={articleIds} id={article.id} name={article.title} more={articles.hasNextPage} busy={moves.moving} onAnnounce={moves.announce}
                  focus={moves.focus} onMove={(_next, step) => moveArticle(ordered, article.id, step)} />} />)}
              {articles.hasNextPage && <li className="kb-more-row"><button type="button" className="kb-more" disabled={articles.isFetchingNextPage} onClick={() => void articles.fetchNextPage()}>
                {articles.isFetchingNextPage ? messages.loading : kbMessages.loadMore}</button></li>}
            </ul>}
          </div>
        </section>
      </div>
      {bulk.toast && <KBToast text={bulk.toast.text} undo={bulk.toast.operation && kbBulkMessages.undo} onUndo={() => void bulk.undo()} onDismiss={bulk.dismiss} />}
    </main>
  );
}

