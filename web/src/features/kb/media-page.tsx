import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { DragEvent, ReactNode } from 'react';
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { SearchIcon } from '../../ui/icons';
import { RouterLink } from '../../ui/router-link';
import { typingDelayMs } from '../inbox/inbox-filters';
import { Icon } from './editor/icons';
import { kbApi } from './kb-api';
import { reason } from './kb-tree';
import { KBDialog, KBHead, KBToast, usePhone } from './kb-ui';
import { mediaApi } from './media-api';
import type { KBMedia, KBMediaUpload, MediaKind, MediaSort, MediaUsage } from './media-api';
import { MediaDetail } from './media-detail';
import { MediaCard, SelectCard, mediaName, usageLabel } from './media-grid';
import { mediaMessages } from './media-messages';
import { baseName, useUploadQueue } from './media-uploads';
import type { UploadQueue, UploadRow } from './media-uploads';

export interface MediaSearch {
  q?: string;
  kind?: MediaKind;
  usage?: MediaUsage;
  sort?: MediaSort;
  item?: string;
}
export type MediaView = 'library' | 'unused' | 'trash';

const viewPath = { library: '/knowledge/media', unused: '/knowledge/media/unused', trash: '/knowledge/media/trash' } as const;
const accept = 'image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,video/quicktime,.mov';

// Every change to the library can move items between the pages, so all of them are read again.
function useRefreshMedia() {
  const client = useQueryClient();
  return useCallback(() => Promise.all([client.invalidateQueries({ queryKey: ['kb-media'] }), client.invalidateQueries({ queryKey: ['kb-media-trash'] }),
    client.invalidateQueries({ queryKey: ['kb-media-details'] })]), [client]);
}

export function MediaRoute({ view, search }: { view: MediaView; search: MediaSearch }) {
  const navigate = useNavigate();
  const refresh = useRefreshMedia();
  const current = useRef(search);
  current.current = search;
  // go is stable: the cards are memoized, and a new function each render would draw them all again.
  const latest = useRef({ navigate, view });
  latest.current = { navigate, view };
  // Opening or closing an item keeps the grid where it was scrolled (on a phone the item is a sheet over it).
  const go = useCallback((to: MediaView, change: Partial<MediaSearch> = {}, replace = false) =>
    void latest.current.navigate({ to: viewPath[to], search: to === latest.current.view ? { ...current.current, ...change } : change, replace,
      ...('item' in change ? { resetScroll: false } : {}) }), []);
  const { queue, rows } = useUploadQueue(() => void refresh());
  const add = (files: File[]) => { queue.add(files); };
  const [dragging, setDragging] = useState(false);
  const drop = {
    onDragOver: (event: DragEvent) => { if (event.dataTransfer.types.includes('Files')) { event.preventDefault(); setDragging(true); } },
    onDragLeave: (event: DragEvent) => { if (event.currentTarget === event.target) setDragging(false); },
    onDrop: (event: DragEvent) => { event.preventDefault(); setDragging(false); add(Array.from(event.dataTransfer.files)); },
  };
  const file = useRef<HTMLInputElement>(null);
  const choose = () => file.current?.click();
  const collections = useQuery({ queryKey: ['kb-collections'], queryFn: kbApi.collections });
  const articles = collections.data?.items.reduce((sum, item) => sum + item.article_count, 0);
  const heading = view === 'unused' ? mediaMessages.unusedHeading : view === 'trash' ? mediaMessages.trashHeading : mediaMessages.heading;
  return (
    <main className="kb-frame media-page" aria-labelledby="kb-heading" {...drop} data-dragging={dragging ? 'true' : undefined} data-view={view}>
      <KBHead current="media" heading={heading} articles={articles} actions={<>
        {view !== 'trash' && <RouterLink className="button-link" href={viewPath.trash} label={mediaMessages.trash} onFollow={() => go('trash')}><Icon name="trash" /><span className="kb-label">{mediaMessages.trash}</span></RouterLink>}
        {view === 'library' && <button type="button" className="primary-action" title={mediaMessages.uploadHint} aria-describedby="media-upload-hint" onClick={choose}><Icon name="upload" />{mediaMessages.upload}</button>}
        <span id="media-upload-hint" className="sr-only">{mediaMessages.uploadHint}</span>
        <input ref={file} type="file" hidden multiple accept={accept} onChange={(event) => { add(Array.from(event.target.files ?? [])); event.target.value = ''; }} />
      </>} />
      {dragging && <p className="media-drop" aria-hidden="true">{mediaMessages.dropHere}</p>}
      {view === 'library' && <Library search={search} go={go} onChanged={() => void refresh()} onUpload={choose}
        tray={<UploadTray queue={queue} rows={rows} onShow={(id) => go('library', { item: id })} />} />}
      {view === 'unused' && <Unused go={go} onChanged={() => void refresh()} />}
      {view === 'trash' && <Trash go={go} onChanged={() => void refresh()} />}
    </main>
  );
}

// UploadTray floats over the corner of the grid and never pushes the library down. Collapsed it is one line
// with a bar split by outcome; it opens on its own when a file fails, so a failure is never missed. Its rows
// are this page's uploads and the workspace's unfinished ones from elsewhere (another tab, a page that
// closed), which hold the three a workspace may have, with a way to go on or to cancel.
function UploadTray({ queue, rows, onShow }: { queue: UploadQueue; rows: UploadRow[]; onShow: (id: string) => void }) {
  const uploads = useQuery({ queryKey: ['kb-media-uploads', rows.map((row) => `${row.key}:${row.state}`).join()], queryFn: mediaApi.uploads, placeholderData: keepPreviousData });
  const mine = queue.sessions();
  const others = (uploads.data?.items ?? []).filter((upload) => !mine.has(upload.id));
  const failed = rows.filter((row) => row.state === 'failed').length;
  const [open, setOpen] = useState(false);
  const [hidden, setHidden] = useState(false);
  // A new failure opens the tray; a new file shows it again after it was hidden.
  const seen = useRef({ failed: 0, count: 0 });
  useEffect(() => {
    if (failed > seen.current.failed) { setOpen(true); setHidden(false); }
    if (rows.length > seen.current.count) setHidden(false);
    seen.current = { failed, count: rows.length };
  }, [failed, rows.length]);
  if ((rows.length === 0 && others.length === 0) || hidden) return null;
  const done = rows.filter((row) => row.state === 'done');
  const duplicates = done.filter((row) => row.duplicate).length;
  const running = rows.filter((row) => row.state === 'sending' || row.state === 'waiting').length;
  // The tray closes only when nothing is on its way: an upload still going or paused stays in sight, and
  // closing clears what is finished, so nothing is hidden that still needs the person.
  const settled = rows.every((row) => !['waiting', 'sending', 'paused'].includes(row.state));
  const total = rows.length;
  const at = total - running + (running > 0 ? 1 : 0);
  const title = running > 0 ? mediaMessages.uploadingOf(Math.min(at, total), total) : total > 0 && failed === 0 ? mediaMessages.uploadsFinished(total) : mediaMessages.uploads;
  const part = (count: number) => `${total ? (count / total) * 100 : 0}%`;
  return (
    <section className="media-tray" aria-labelledby="media-tray-title" data-open={open ? 'true' : undefined}>
      <div className="media-tray-head">
        <h2 id="media-tray-title" aria-live="polite">{title}</h2>
        <span className="media-tray-sub">
          {done.length - duplicates > 0 && <span>{mediaMessages.added(done.length - duplicates)}</span>}
          {duplicates > 0 && <span>{mediaMessages.alreadyIn(duplicates)}</span>}
          {failed > 0 && <span className="media-tray-bad">{mediaMessages.notUploaded(failed)}</span>}
          {others.length > 0 && <span>{mediaMessages.elsewhere(others.length)}</span>}
        </span>
        <span className="media-tray-controls">
          {open && done.length > 0 && <button type="button" className="link-action" onClick={() => queue.clear()}>{mediaMessages.clearFinished}</button>}
          <button type="button" className="icon-button" aria-expanded={open} aria-controls="media-tray-list" aria-label={open ? mediaMessages.collapseUploads : mediaMessages.showUploads(total + others.length)}
            onClick={() => setOpen(!open)}><Icon name={open ? 'chevronDown' : 'chevronUp'} /></button>
          {settled && <button type="button" className="icon-button" aria-label={mediaMessages.closeUploads} onClick={() => { for (const row of rows) queue.dismiss(row.key); setHidden(true); }}><Icon name="close" /></button>}
        </span>
        {total > 0 && <span className="media-tray-bar" aria-hidden="true">
          <i data-part="done" style={{ width: part(done.length) }} /><i data-part="running" style={{ width: part(running) }} /><i data-part="failed" style={{ width: part(failed) }} />
        </span>}
      </div>
      <div id="media-tray-list" className="media-tray-list" hidden={!open}>
        {rows.length > 0 && <ul aria-label={mediaMessages.uploads}>
          {rows.map((row) => <TrayRow key={row.key} row={row} queue={queue} onShow={onShow} />)}
        </ul>}
        {others.length > 0 && <InProgress queue={queue} items={others} onChanged={() => void uploads.refetch()} />}
      </div>
    </section>
  );
}

function TrayRow({ row, queue, onShow }: { row: UploadRow; queue: UploadQueue; onShow: (id: string) => void }) {
  const percent = row.size ? Math.min(100, Math.round((row.sent / row.size) * 100)) : 0;
  const left = row.bytesPerSecond > 0 ? (row.size - row.sent) / row.bytesPerSecond : undefined;
  const status = row.state === 'done' ? (row.duplicate ? mediaMessages.alreadyInLibrary : mediaMessages.uploaded)
    : row.state === 'sending' ? [`${percent}%`, mediaMessages.sending(row.sent, row.size), row.kind === 'video' && row.bytesPerSecond > 0 ? mediaMessages.speed(row.bytesPerSecond) : '',
      left !== undefined && row.kind === 'video' ? mediaMessages.timeLeft(left) : ''].filter(Boolean).join(' · ')
    : row.state === 'paused' ? `${mediaMessages.paused} · ${mediaMessages.sending(row.sent, row.size)}` : row.state === 'waiting' ? mediaMessages.waiting
    : row.state === 'cancelled' ? mediaMessages.cancelled : row.problem;
  const icon = row.state === 'failed' ? 'alert' : row.state === 'done' ? 'check' : row.state === 'paused' ? 'pause' : row.state === 'cancelled' ? 'close' : 'upload';
  return (
    <li className="media-tray-row" data-state={row.state} data-duplicate={row.duplicate ? 'true' : undefined}>
      <span className="media-tray-icon" aria-hidden="true"><Icon name={icon} size={12} /></span>
      <span className="media-upload-name" title={row.name}>{row.name}</span>
      <span className="media-tray-actions">
        {row.kind === 'video' && (row.state === 'sending' || row.state === 'waiting') && <button type="button" onClick={() => queue.pause(row.key)} aria-label={mediaMessages.pauseName(row.name)}>{mediaMessages.pause}</button>}
        {(row.state === 'paused' || (row.state === 'failed' && !row.permanent)) && <button type="button" onClick={() => queue.resume(row.key)} aria-label={mediaMessages.resumeName(row.name)}>{row.state === 'failed' ? mediaMessages.retry : mediaMessages.resume}</button>}
        {['waiting', 'sending', 'paused'].includes(row.state) && <button type="button" onClick={() => queue.cancel(row.key)} aria-label={mediaMessages.cancelName(row.name)}>{mediaMessages.cancel}</button>}
        {row.state === 'done' && row.item && <button type="button" onClick={() => onShow(row.item!.id)} aria-label={mediaMessages.showName(row.name)}>{mediaMessages.show}</button>}
        {(row.state === 'failed' || row.state === 'cancelled') && <button type="button" onClick={() => queue.dismiss(row.key)} aria-label={mediaMessages.dismissName(row.name)}>{mediaMessages.dismiss}</button>}
      </span>
      <span className="media-upload-status" role={row.state === 'failed' ? 'alert' : undefined}>{status}</span>
      {(row.state === 'sending' || row.state === 'paused') && <span className="media-tray-progress" role="progressbar" aria-label={row.name} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}><i style={{ width: `${percent}%` }} /></span>}
    </li>
  );
}

// InProgress lists the workspace's uploads this page is not sending: what holds the three a workspace may
// have, with a way to go on (the same file again) or to cancel (for whoever started it, or an admin).
function InProgress({ queue, items, onChanged }: { queue: UploadQueue; items: KBMediaUpload[]; onChanged: () => void }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const [now] = useState(() => new Date());
  const [problem, setProblem] = useState<string>();
  const file = useRef<HTMLInputElement>(null);
  const resuming = useRef<KBMediaUpload | undefined>(undefined);
  const timezone = me.data?.timezone ?? 'UTC';
  const admin = me.data?.agent.role === 'admin';
  const cancel = async (upload: KBMediaUpload) => {
    setProblem(undefined);
    try {
      await mediaApi.cancelUpload(upload.id);
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  return (
    <section className="media-in-progress" aria-labelledby="media-in-progress-heading">
      <h3 id="media-in-progress-heading">{mediaMessages.inProgress}</h3>
      <p className="kb-hint">{mediaMessages.inProgressHelp}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <ul>
        {items.map((upload) => (
          <li key={upload.id} className="media-tray-row" data-state="paused">
            <span className="media-tray-icon" aria-hidden="true"><Icon name="pause" size={12} /></span>
            <span className="media-upload-name" title={upload.name}>{upload.name || mediaMessages.unnamed}</span>
            <span className="media-tray-actions">
              <button type="button" aria-label={mediaMessages.resumeChooseName(upload.name)} onClick={() => { resuming.current = upload; file.current?.click(); }}>{mediaMessages.resumeChoose}</button>
              {(admin || upload.created_by?.agent_id === me.data?.agent.id) && <button type="button" aria-label={mediaMessages.cancelUploadName(upload.name)} onClick={() => void cancel(upload)}>{mediaMessages.cancel}</button>}
            </span>
            <span className="media-upload-status">{mediaMessages.inProgressRow(Math.floor((upload.offset / upload.size) * 100), upload.created_by?.name ?? mediaMessages.someone,
              formatDate(upload.expires_at, timezone, now))}</span>
          </li>
        ))}
      </ul>
      <input ref={file} type="file" hidden accept="video/mp4,video/webm,video/quicktime,.mov" onChange={(event) => {
        const chosen = event.target.files?.[0];
        event.target.value = '';
        const upload = resuming.current;
        if (!chosen || !upload) return;
        // The server knows the name and size, not the bytes: both must match, and complete checks the file.
        if (chosen.size !== upload.size || baseName(chosen.name).trim() !== upload.name) { setProblem(mediaMessages.notTheSameFile(upload.name)); return; }
        setProblem(undefined);
        queue.resumeUpload(chosen, upload.id, upload.name || chosen.name);
      }} />
    </section>
  );
}

function useMediaList(filter: Omit<MediaSearch, 'item'>) {
  return useInfiniteQuery({
    queryKey: ['kb-media', filter],
    queryFn: ({ pageParam, signal }) => mediaApi.list({ ...filter, cursor: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  });
}

type Go = (to: MediaView, change?: Partial<MediaSearch>, replace?: boolean) => void;

function UseSelect({ search, go }: { search: MediaSearch; go: Go }) {
  return (
    <select aria-label={mediaMessages.usage} value={search.usage ?? ''} onChange={(event) => go('library', { usage: (event.target.value || undefined) as MediaUsage | undefined })}>
      <option value="">{mediaMessages.anyUse}</option>
      <option value="in_use">{mediaMessages.inUse}</option>
      <option value="unused">{mediaMessages.unused}</option>
      <option value="missing_alt">{mediaMessages.missingAlt}</option>
    </select>
  );
}

function SortSelect({ search, go }: { search: MediaSearch; go: Go }) {
  return (
    <select aria-label={mediaMessages.sort} value={search.sort ?? 'newest'} onChange={(event) => go('library', { sort: event.target.value === 'newest' ? undefined : event.target.value as MediaSort })}>
      <option value="newest">{mediaMessages.newestFirst}</option>
      <option value="oldest">{mediaMessages.oldestFirst}</option>
      <option value="name">{mediaMessages.name}</option>
      <option value="largest">{mediaMessages.largest}</option>
    </select>
  );
}

function Library({ search, go, onChanged, onUpload, tray }: { search: MediaSearch; go: Go; onChanged: () => void; onUpload: () => void; tray: ReactNode }) {
  const filter = { q: search.q, kind: search.kind, usage: search.usage, sort: search.sort };
  const list = useMediaList(filter);
  // The size of what no article uses, for the line that leads to the Not used page.
  const unused = useQuery({ queryKey: ['kb-media', 'unused-summary'], queryFn: ({ signal }) => mediaApi.list({ usage: 'unused', sort: 'largest' }, signal) });
  const summary = unused.data?.summary;
  const phone = usePhone();
  const [filters, setFilters] = useState(false);
  const [text, setText] = useState(search.q ?? '');
  useEffect(() => setText(search.q ?? ''), [search.q]);
  const typed = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(typed.current), []);
  const type = (value: string) => {
    setText(value);
    window.clearTimeout(typed.current);
    typed.current = window.setTimeout(() => go('library', { q: value.trim() || undefined }, true), typingDelayMs);
  };
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  const filtered = Boolean(search.q || search.kind || search.usage);
  const empty = list.isSuccess && items.length === 0 && !filtered;
  const openNow = useRef(search.item);
  openNow.current = search.item;
  const openItem = useCallback((chosen: KBMedia) => go('library', { item: chosen.id === openNow.current ? undefined : chosen.id }), [go]);
  // Closing gives the focus back to the card the panel was opened from.
  const closeItem = useCallback(() => {
    const closed = openNow.current;
    go('library', { item: undefined });
    requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-media-id="${closed}"]`)?.focus());
  }, [go]);
  const all = summary ? summary.count + summary.kept_count : 0;
  return (
    <div className="media-work" data-empty={empty ? 'true' : undefined} data-open={search.item ? 'true' : undefined}>
      <section className="media-library" aria-labelledby="media-list-heading">
        <h2 id="media-list-heading" className="sr-only">{mediaMessages.libraryHeading}</h2>
        {summary && all > 0 && <p className="media-ledger">
          <span><strong>{mediaMessages.notUsedCount(all)}</strong> · {formatBytes(summary.bytes + summary.kept_bytes)}{summary.kept_count > 0 ? `, ${mediaMessages.ofThemKept(summary.kept_count)}` : ''}</span>
          <RouterLink className="media-review" href={viewPath.unused} onFollow={() => go('unused')}>{mediaMessages.reviewNotUsed}</RouterLink>
        </p>}
        {!empty && <div className="media-toolbar" role="search">
          <label className="search-field media-search">
            <SearchIcon />
            <span className="sr-only">{mediaMessages.search}</span>
            <input type="search" value={text} placeholder={mediaMessages.searchPlaceholder} maxLength={200} aria-describedby="media-search-hint" onChange={(event) => type(event.target.value)} />
          </label>
          <span id="media-search-hint" className="sr-only">{mediaMessages.searchHint}</span>
          <fieldset className="segmented">
            <legend className="sr-only">{mediaMessages.kind}</legend>
            {([[undefined, mediaMessages.anyKind], ['image', mediaMessages.images], ['video', mediaMessages.videos]] as const).map(([value, label]) => (
              <label key={label}><input type="radio" name="media-kind" checked={search.kind === value} onChange={() => go('library', { kind: value })} />{label}</label>
            ))}
          </fieldset>
          {!phone && <><UseSelect search={search} go={go} /><SortSelect search={search} go={go} /></>}
          {phone && <button type="button" className="media-filter-button" aria-haspopup="dialog" aria-label={mediaMessages.filterLabel(search.usage, search.sort)} onClick={() => setFilters(true)}>
            <Icon name="filter" />{mediaMessages.filter}</button>}
        </div>}
        <div className="media-grid-scroll">
          {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}{' '}
            <button type="button" className="link-action" onClick={() => void list.refetch()}>{mediaMessages.retry}</button></p>}
          {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
          {list.isSuccess && items.length === 0 && filtered && <p className="empty-copy">{mediaMessages.noMatches}</p>}
          {empty && <div className="media-empty">
            <h3>{mediaMessages.emptyHeading}</h3>
            <p>{mediaMessages.emptyHelp}</p>
            <button type="button" className="primary-action" onClick={onUpload}><Icon name="upload" />{mediaMessages.uploadFiles}</button>
          </div>}
          {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.libraryHeading} aria-busy={list.isFetching && !list.isFetchingNextPage}>
            {items.map((item) => <MediaCard key={item.id} item={item} open={item.id === search.item} onOpen={openItem} />)}
            {list.hasNextPage && <li className="media-more"><button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
              {list.isFetchingNextPage ? mediaMessages.loading : mediaMessages.loadMore}</button></li>}
          </ul>}
        </div>
        {tray}
      </section>
      {!empty && <MediaDetail id={search.item} onChanged={onChanged} onClose={closeItem} />}
      {filters && <KBDialog title={mediaMessages.useAndSort} closeLabel={mediaMessages.close} onClose={() => setFilters(false)}
        footer={<button type="button" className="primary-action" onClick={() => setFilters(false)}>{mediaMessages.done}</button>}>
        <label className="field">{mediaMessages.usage}<UseSelect search={search} go={go} /></label>
        <label className="field">{mediaMessages.sort}<SortSelect search={search} go={go} /></label>
      </KBDialog>}
    </div>
  );
}

// useSelection ticks items of a list; a Shift-click ticks the range from the last one ticked.
function useSelection(items: KBMedia[]) {
  const [ids, setIds] = useState<Set<string>>(new Set());
  const last = useRef<string | undefined>(undefined);
  const list = useRef(items);
  list.current = items;
  // Stable, so a tick draws the two cards that change, not the page.
  const toggle = useCallback((item: KBMedia, range: boolean) => {
    // Read now: the updater runs later, when last already names this item.
    const anchor = range ? last.current : undefined;
    const all = list.current;
    setIds((before) => {
      const next = new Set(before);
      const from = anchor ? all.findIndex((each) => each.id === anchor) : -1;
      const to = all.findIndex((each) => each.id === item.id);
      if (from >= 0 && to >= 0) {
        const on = !before.has(item.id);
        for (const each of all.slice(Math.min(from, to), Math.max(from, to) + 1)) { if (on) next.add(each.id); else next.delete(each.id); }
      } else if (next.has(item.id)) next.delete(item.id);
      else next.add(item.id);
      return next;
    });
    last.current = item.id;
  }, []);
  const chosen = items.filter((item) => ids.has(item.id));
  return { ids, chosen, toggle, all: () => setIds(new Set(items.map((item) => item.id))), clear: () => setIds(new Set()) };
}

// SelectBar ticks or clears everything shown, says what is ticked, and holds the page's actions.
function SelectBar({ chosen, total, onAll, onClear, children }: { chosen: KBMedia[]; total: number; onAll: () => void; onClear: () => void; children: ReactNode }) {
  const box = useRef<HTMLInputElement>(null);
  const some = chosen.length > 0 && chosen.length < total;
  useEffect(() => { if (box.current) box.current.indeterminate = some; }, [some]);
  const bytes = chosen.reduce((sum, item) => sum + item.size, 0);
  return (
    <div className="media-selectbar">
      <label className="media-select-all">
        <input ref={box} type="checkbox" checked={total > 0 && chosen.length === total} aria-label={mediaMessages.selectAll(total)}
          onChange={() => (chosen.length === total ? onClear() : onAll())} />
        <span className="media-select-count">{chosen.length > 0 ? <><span className="media-wide">{mediaMessages.selectedOf(chosen.length, total, bytes)}</span><span className="media-narrow">{mediaMessages.selected(chosen.length)}</span></>
          : mediaMessages.selectAll(total)}</span>
      </label>
      <span className="media-grow" />
      {chosen.length > 0 && <button type="button" className="media-clear" onClick={onClear}>{mediaMessages.clearSelection}</button>}
      {children}
    </div>
  );
}

type Show = 'all' | 'auto' | 'kept';
interface Confirming { all: boolean; count: number; bytes: number }

function Unused({ go, onChanged }: { go: Go; onChanged: () => void }) {
  // The moment the page opened: "Move all" leaves out what was uploaded after it.
  const [opened] = useState(() => new Date().toISOString());
  const list = useMediaList({ usage: 'unused', sort: 'largest' });
  const loaded = useMemo(() => list.data?.pages.flatMap((page) => page.items) ?? [], [list.data]);
  const summary = list.data?.pages[0]?.summary;
  const [show, setShow] = useState<Show>('all');
  const items = useMemo(() => (show === 'all' ? loaded : loaded.filter((item) => item.kept === (show === 'kept'))), [loaded, show]);
  const selection = useSelection(items);
  const [confirming, setConfirming] = useState<Confirming>();
  const [explaining, setExplaining] = useState(false);
  const [done, setDone] = useState<{ text: string; batch?: string }>();
  const [problem, setProblem] = useState<string>();
  const [now] = useState(() => new Date());
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timezone = me.data?.timezone ?? 'UTC';
  // The count is what the list holds: items cleaned up on their own and kept ones, said apart.
  const auto = summary?.count ?? 0;
  const kept = summary?.kept_count ?? 0;
  const counts: Record<Show, number> = { all: auto + kept, auto, kept };
  const bytes: Record<Show, number> = { all: (summary?.bytes ?? 0) + (summary?.kept_bytes ?? 0), auto: summary?.bytes ?? 0, kept: summary?.kept_bytes ?? 0 };
  const firstPurge = loaded.filter((item) => !item.kept && item.purge_at).map((item) => item.purge_at!).sort()[0];
  const move = async () => {
    if (!confirming) return;
    setProblem(undefined);
    try {
      if (confirming.all) {
        let batch: string | undefined;
        let moved = 0;
        try {
          for (let more = true; more;) {
            const answer = await mediaApi.trashUnused({ before: opened, include_kept: show === 'all', ...(batch ? { batch_id: batch } : {}) });
            moved += answer.trashed;
            batch = answer.batch_id ?? batch;
            more = answer.more;
          }
        } finally {
          // A failure halfway still offers Undo for what moved.
          if (moved > 0) setDone({ text: mediaMessages.moved(moved), batch });
        }
      } else {
        let batch: string | undefined;
        let moved = 0;
        let stayed = 0;
        const ids = selection.chosen.map((item) => item.id);
        for (let at = 0; at < ids.length; at += 100) {
          const answer = await mediaApi.moveToTrash({ ids: ids.slice(at, at + 100), ...(batch ? { batch_id: batch } : {}) });
          batch = answer.batch_id ?? batch;
          moved += answer.results.filter((result) => result.status === 'trashed').length;
          stayed += answer.results.filter((result) => result.status === 'in_use').length;
        }
        setDone({ text: [mediaMessages.moved(moved), stayed ? mediaMessages.movedSkipped(stayed) : ''].filter(Boolean).join(' '), batch });
      }
      selection.clear();
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    } finally {
      setConfirming(undefined);
    }
  };
  const undo = async () => {
    if (!done?.batch) return;
    try {
      const { results } = await mediaApi.restore({ batch_id: done.batch });
      setDone({ text: mediaMessages.restored(results.filter((result) => result.status === 'restored').length) });
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  const chosenBytes = selection.chosen.reduce((sum, item) => sum + item.size, 0);
  // Nothing ticked moves everything the filter shows; the kept view moves only what is ticked, since the
  // server's "all" always takes the items nobody kept.
  const canMoveAll = show !== 'kept' && counts[show] > 0;
  return (
    <section className="media-unused" aria-labelledby="kb-heading">
      <nav className="kb-crumbs" aria-label={mediaMessages.breadcrumb}>
        <RouterLink href={viewPath.library} onFollow={() => go('library')}>{mediaMessages.heading}</RouterLink><span aria-hidden="true">/</span>
        <span aria-current="page" className="media-crumb-title">{mediaMessages.unusedHeading}</span>
      </nav>
      {summary && <div className="kb-panel media-explain">
        <div>
          <p className="media-explain-big"><strong>{mediaMessages.itemsAndBytes(counts.all, bytes.all)}</strong> {mediaMessages.inNoLiveArticle}</p>
          <p>{auto > 0 && <>{mediaMessages.autoCleanup(auto, firstPurge ? formatDate(firstPurge, timezone, now) : undefined)} </>}
            {kept > 0 && <><span className="media-kept-legend"><Icon name="pin" size={14} />{mediaMessages.areKept(kept)}</span>{mediaMessages.keptReason}</>}</p>
        </div>
        <button type="button" aria-haspopup="dialog" onClick={() => setExplaining(true)}>{mediaMessages.howCleanup}</button>
      </div>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {loaded.length > 0 && <div className="media-unused-tools">
        <fieldset className="segmented">
          <legend className="sr-only">{mediaMessages.showFilter}</legend>
          {([['all', mediaMessages.anyKind], ['auto', mediaMessages.autoCleanupFilter], ['kept', mediaMessages.kept]] as const).map(([value, label]) => (
            <label key={value}><input type="radio" name="media-unused-show" checked={show === value} onChange={() => { setShow(value); selection.clear(); }} />{label}
              <span className="kb-count-tag">{counts[value].toLocaleString('en')}</span></label>
          ))}
        </fieldset>
        <SelectBar chosen={selection.chosen} total={items.length} onAll={selection.all} onClear={selection.clear}>
          {selection.chosen.length > 0
            ? <button type="button" className="danger-solid" onClick={() => setConfirming({ all: false, count: selection.chosen.length, bytes: chosenBytes })}><Icon name="trash" />{mediaMessages.moveCount(selection.chosen.length)}</button>
            : canMoveAll
              ? <button type="button" className="danger-solid" onClick={() => setConfirming({ all: true, count: counts[show], bytes: bytes[show] })}><Icon name="trash" />{mediaMessages.moveAllCount(counts[show])}</button>
              : <span className="kb-hint">{mediaMessages.tickToMove}</span>}
        </SelectBar>
      </div>}
      <div className="media-grid-scroll">
        {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}{' '}
          <button type="button" className="link-action" onClick={() => void list.refetch()}>{mediaMessages.retry}</button></p>}
        {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
        {list.isSuccess && loaded.length === 0 && <p className="empty-copy">{mediaMessages.noUnused}</p>}
        {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.unusedHeading}>
          {items.map((item) => <SelectCard key={item.id} item={item} checked={selection.ids.has(item.id)} onToggle={selection.toggle}
            extra={unusedReason(item, timezone, now)} />)}
          {list.hasNextPage && <li className="media-more"><button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {list.isFetchingNextPage ? mediaMessages.loading : mediaMessages.loadMore}</button></li>}
        </ul>}
      </div>
      {confirming && <ConfirmDialog heading={mediaMessages.confirmMoveHeading(confirming.count)} confirm={mediaMessages.moveCount(confirming.count)} onConfirm={move} onCancel={() => setConfirming(undefined)}>
        <p className="muted-copy">{mediaMessages.confirmBody(confirming.bytes)}</p>
        {!confirming.all && <Thumbs items={selection.chosen} />}
        {!confirming.all && <ConfirmUsage items={selection.chosen} />}
        {confirming.all
          ? kept > 0 && <p className="media-warn"><Icon name="pin" />{show === 'all' ? mediaMessages.keptGoToo(kept) : mediaMessages.confirmKeptSkipped}</p>
          : selection.chosen.some((item) => item.kept) && <p className="media-warn"><Icon name="pin" />{mediaMessages.keptChosen(selection.chosen.filter((item) => item.kept).map(mediaName))}</p>}
      </ConfirmDialog>}
      {explaining && <KBDialog title={mediaMessages.howCleanup} closeLabel={mediaMessages.close} onClose={() => setExplaining(false)}
        footer={<button type="button" className="primary-action" onClick={() => setExplaining(false)}>{mediaMessages.done}</button>}>
        {mediaMessages.cleanupRules.map((text) => <p key={text}>{text}</p>)}
      </KBDialog>}
      {done && <KBToast text={done.text} undo={done.batch ? mediaMessages.undo : undefined} onUndo={() => void undo()} onDismiss={() => setDone(undefined)} />}
    </section>
  );
}

// unusedReason says on a card why the item is on the Not used page.
function unusedReason(item: KBMedia, timezone: string, now: Date) {
  const where = usageLabel(item);
  const date = formatDate(item.created_at, timezone, now);
  if (item.kept) return where && where !== mediaMessages.notUsed ? `${mediaMessages.kept} · ${where}` : mediaMessages.keptNeverUsed(date);
  if (where && where !== mediaMessages.notUsed) return where;
  return item.purge_at ? mediaMessages.purgesOn(formatDate(item.purge_at, timezone, now)) : mediaMessages.notUsed;
}

function Thumbs({ items }: { items: KBMedia[] }) {
  return (
    <ul className="media-thumbs">
      {items.slice(0, 6).map((item) => (
        <li key={item.id}>
          {item.thumbnail_url ? <img src={item.thumbnail_url} alt="" /> : <span className="media-picture-empty" aria-hidden="true" />}
          <span title={mediaName(item)}>{mediaName(item)}</span>
        </li>
      ))}
      {items.length > 6 && <li className="media-thumbs-more">{mediaMessages.andMore(items.length - 6)}</li>}
    </ul>
  );
}

// ConfirmUsage counts the chosen items that only deleted articles or older versions still name.
function ConfirmUsage({ items }: { items: KBMedia[] }) {
  const trashed = items.filter((item) => (item.usage?.trashed_articles ?? 0) > 0).length;
  const older = items.filter((item) => (item.usage?.older_versions ?? 0) > 0 && (item.usage?.trashed_articles ?? 0) === 0).length;
  return <>
    {trashed > 0 && <p className="media-warn"><Icon name="info" />{mediaMessages.inDeletedArticle(trashed)}</p>}
    {older > 0 && <p className="media-warn"><Icon name="info" />{mediaMessages.inOlderVersion(older)}</p>}
  </>;
}

function ConfirmDialog({ heading, confirm, children, onConfirm, onCancel }: { heading: string; confirm: string; children: ReactNode; onConfirm: () => Promise<void>; onCancel: () => void }) {
  // One press sends once: the button waits for the answer.
  const [busy, setBusy] = useState(false);
  return (
    <KBDialog title={heading} role="alertdialog" onClose={onCancel} footer={<>
      <button type="button" onClick={onCancel}>{mediaMessages.cancel}</button>
      <button type="button" className="danger-solid" disabled={busy} aria-busy={busy}
        onClick={() => { setBusy(true); void onConfirm().finally(() => setBusy(false)); }}>{confirm}</button>
    </>}>
      {children}
    </KBDialog>
  );
}

// LostIn names what deleting forever leaves without its media: deleted articles and older versions
// that still show an item then say "Media no longer exists".
function LostIn({ items }: { items: KBMedia[] }) {
  const trashed = items.reduce((sum, item) => sum + (item.usage?.trashed_articles ?? 0), 0);
  const older = items.reduce((sum, item) => sum + (item.usage?.older_versions ?? 0), 0);
  return trashed + older > 0 ? <p>{mediaMessages.lostIn(trashed, older)}</p> : null;
}

function Trash({ go, onChanged }: { go: Go; onChanged: () => void }) {
  const list = useInfiniteQuery({
    queryKey: ['kb-media-trash'],
    queryFn: ({ pageParam }) => mediaApi.trash(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  const selection = useSelection(items);
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const admin = me.data?.agent.role === 'admin';
  const timezone = me.data?.timezone ?? 'UTC';
  const [now] = useState(() => new Date());
  const [deleting, setDeleting] = useState(false);
  const [done, setDone] = useState<string>();
  const [problem, setProblem] = useState<string>();
  const run = async (action: 'restore' | 'purge') => {
    setProblem(undefined);
    try {
      const ids = selection.chosen.map((item) => item.id);
      let count = 0;
      for (let at = 0; at < ids.length; at += 100) {
        const chunk = ids.slice(at, at + 100);
        const { results } = action === 'restore' ? await mediaApi.restore({ ids: chunk }) : await mediaApi.purge(chunk);
        count += results.filter((result) => result.status === (action === 'restore' ? 'restored' : 'purged')).length;
      }
      setDone(action === 'restore' ? mediaMessages.restored(count) : mediaMessages.deleted(count));
      selection.clear();
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    } finally {
      setDeleting(false);
    }
  };
  const bytes = selection.chosen.reduce((sum, item) => sum + item.size, 0);
  return (
    <section className="media-unused" aria-labelledby="kb-heading">
      <nav className="kb-crumbs" aria-label={mediaMessages.breadcrumb}>
        <RouterLink href={viewPath.library} onFollow={() => go('library')}>{mediaMessages.heading}</RouterLink><span aria-hidden="true">/</span>
        <span aria-current="page" className="media-crumb-title">{mediaMessages.trashHeading}</span>
      </nav>
      <p className="muted-copy media-trash-help">{mediaMessages.trashDescription}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {items.length > 0 && <div className="media-unused-tools">
        <SelectBar chosen={selection.chosen} total={items.length} onAll={selection.all} onClear={selection.clear}>
          <button type="button" disabled={selection.chosen.length === 0} onClick={() => void run('restore')}>{mediaMessages.restoreSelected}</button>
          <button type="button" className="danger-action" disabled={!admin || selection.chosen.length === 0} aria-describedby={admin ? undefined : 'media-admins-only'}
            onClick={() => setDeleting(true)}>{mediaMessages.deleteForever}</button>
          {!admin && <span id="media-admins-only" className="kb-hint">{mediaMessages.adminsOnly}</span>}
        </SelectBar>
      </div>}
      <div className="media-grid-scroll">
        {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
        {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}</p>}
        {list.isSuccess && items.length === 0 && <p className="empty-copy">{mediaMessages.trashEmpty}</p>}
        {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.trashHeading}>
          {items.map((item) => <SelectCard key={item.id} item={item} checked={selection.ids.has(item.id)} onToggle={selection.toggle}
            extra={item.deleted_at ? mediaMessages.goneOn(formatDate(new Date(Date.parse(item.deleted_at) + 30 * 86_400_000).toISOString(), timezone, now)) : undefined} />)}
          {list.hasNextPage && <li className="media-more"><button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{mediaMessages.loadMore}</button></li>}
        </ul>}
      </div>
      {deleting && <ConfirmDialog heading={mediaMessages.deleteForeverHeading} confirm={mediaMessages.deleteForeverConfirm(selection.chosen.length)}
        onConfirm={() => run('purge')} onCancel={() => setDeleting(false)}>
        <p>{mediaMessages.deleteForeverBody(selection.chosen.length, bytes)}</p>
        <LostIn items={selection.chosen} />
        <Thumbs items={selection.chosen} />
      </ConfirmDialog>}
      {done && <KBToast text={done} onDismiss={() => setDone(undefined)} />}
    </section>
  );
}
