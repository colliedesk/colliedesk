import { useCallback, useEffect, useRef, useState } from 'react';
import type { DragEvent, ReactNode } from 'react';
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { SearchIcon } from '../../ui/icons';
import { RouterLink } from '../../ui/router-link';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { typingDelayMs } from '../inbox/inbox-filters';
import { reason } from './kb-tree';
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

// Every change to the library can move items between the pages, so all of them are read again.
function useRefreshMedia() {
  const client = useQueryClient();
  return useCallback(() => Promise.all([client.invalidateQueries({ queryKey: ['kb-media'] }), client.invalidateQueries({ queryKey: ['kb-media-trash'] }),
    client.invalidateQueries({ queryKey: ['kb-media-details'] })]), [client]);
}

// KBSections is the Articles | Media switch at the top of the knowledge base pages.
export function KBSections({ current }: { current: 'articles' | 'media' }) {
  const navigate = useNavigate();
  return (
    <nav className="kb-sections" aria-label={mediaMessages.sections}>
      <RouterLink href="/knowledge" current={current === 'articles'} onFollow={() => void navigate({ to: '/knowledge' })}>{mediaMessages.articlesTab}</RouterLink>
      <RouterLink href="/knowledge/media" current={current === 'media'} onFollow={() => void navigate({ to: '/knowledge/media' })}>{mediaMessages.mediaTab}</RouterLink>
    </nav>
  );
}

export function MediaRoute({ view, search }: { view: MediaView; search: MediaSearch }) {
  const navigate = useNavigate();
  const refresh = useRefreshMedia();
  const current = useRef(search);
  current.current = search;
  const go = (to: MediaView, change: Partial<MediaSearch> = {}, replace = false) =>
    void navigate({ to: viewPath[to], search: to === view ? { ...current.current, ...change } : change, replace });
  const { queue, rows } = useUploadQueue(() => void refresh());
  const [refused, setRefused] = useState<string[]>([]);
  const add = (files: File[]) => setRefused(queue.add(files));
  const [dragging, setDragging] = useState(false);
  const drop = {
    onDragOver: (event: DragEvent) => { if (event.dataTransfer.types.includes('Files')) { event.preventDefault(); setDragging(true); } },
    onDragLeave: (event: DragEvent) => { if (event.currentTarget === event.target) setDragging(false); },
    onDrop: (event: DragEvent) => { event.preventDefault(); setDragging(false); add(Array.from(event.dataTransfer.files)); },
  };
  const file = useRef<HTMLInputElement>(null);
  return (
    <main className="settings-page kb-page media-page" aria-labelledby="media-heading" {...drop} data-dragging={dragging ? 'true' : undefined}>
      <header className="kb-header">
        <div>
          <KBSections current="media" />
          <h1 id="media-heading">{view === 'unused' ? mediaMessages.unusedHeading : view === 'trash' ? mediaMessages.trashHeading : mediaMessages.heading}</h1>
          <p>{view === 'unused' ? mediaMessages.unusedDescription : view === 'trash' ? mediaMessages.trashDescription : mediaMessages.description}</p>
        </div>
        <div className="button-row">
          {view !== 'library' && <RouterLink className="button-link" href={viewPath.library} onFollow={() => go('library')}>{mediaMessages.heading}</RouterLink>}
          {view !== 'trash' && <RouterLink className="button-link" href={viewPath.trash} onFollow={() => go('trash')}>{mediaMessages.trash}</RouterLink>}
          {view === 'library' && <>
            <button type="button" className="primary-action" aria-describedby="media-upload-hint" onClick={() => file.current?.click()}>{mediaMessages.upload}</button>
            <input ref={file} type="file" hidden multiple accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,video/quicktime,.mov"
              onChange={(event) => { add(Array.from(event.target.files ?? [])); event.target.value = ''; }} />
          </>}
        </div>
      </header>
      {view === 'library' && <p id="media-upload-hint" className="kb-hint">{mediaMessages.uploadHint}</p>}
      {dragging && <p className="media-drop" aria-hidden="true">{mediaMessages.dropHere}</p>}
      {refused.length > 0 && <p role="alert" className="error-copy">{mediaMessages.notMedia(refused.join(', '))}</p>}
      {view === 'library' && <InProgress queue={queue} rows={rows} />}
      {rows.length > 0 && <Uploads rows={rows} onPause={(key) => queue.pause(key)} onResume={(key) => queue.resume(key)} onCancel={(key) => queue.cancel(key)} onClear={() => queue.clear()} />}
      {view === 'library' && <Library search={search} go={go} onChanged={() => void refresh()} />}
      {view === 'unused' && <Unused onChanged={() => void refresh()} />}
      {view === 'trash' && <Trash onChanged={() => void refresh()} />}
    </main>
  );
}

// InProgress lists the workspace's uploads this page is not sending: what holds the three a workspace
// may have, with a way to go on (the same file again) or to cancel (for whoever started it, or an admin).
function InProgress({ queue, rows }: { queue: UploadQueue; rows: UploadRow[] }) {
  const uploads = useQuery({ queryKey: ['kb-media-uploads', rows.map((row) => `${row.key}:${row.state}`).join()], queryFn: mediaApi.uploads, placeholderData: keepPreviousData });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const [now] = useState(() => new Date());
  const [problem, setProblem] = useState<string>();
  const file = useRef<HTMLInputElement>(null);
  const resuming = useRef<KBMediaUpload | undefined>(undefined);
  const mine = queue.sessions();
  const items = (uploads.data?.items ?? []).filter((upload) => !mine.has(upload.id));
  if (items.length === 0) return null;
  const timezone = me.data?.timezone ?? 'UTC';
  const admin = me.data?.agent.role === 'admin';
  const cancel = async (upload: KBMediaUpload) => {
    setProblem(undefined);
    try {
      await mediaApi.cancelUpload(upload.id);
      void uploads.refetch();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  return (
    <section className="media-uploads" aria-labelledby="media-in-progress-heading">
      <h2 id="media-in-progress-heading">{mediaMessages.inProgress}</h2>
      <p className="kb-hint">{mediaMessages.inProgressHelp}</p>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <ul>
        {items.map((upload) => (
          <li key={upload.id}>
            <span className="media-upload-name">{upload.name || mediaMessages.unnamed}</span>
            <progress max={upload.size} value={upload.offset} aria-label={upload.name} />
            <span className="media-upload-status">{mediaMessages.inProgressRow(Math.floor((upload.offset / upload.size) * 100), upload.created_by?.name ?? mediaMessages.someone,
              formatDate(upload.expires_at, timezone, now))}</span>
            <span className="button-row">
              <button type="button" aria-label={mediaMessages.resumeChooseName(upload.name)} onClick={() => { resuming.current = upload; file.current?.click(); }}>{mediaMessages.resumeChoose}</button>
              {(admin || upload.created_by?.agent_id === me.data?.agent.id) && <button type="button" aria-label={mediaMessages.cancelUploadName(upload.name)} onClick={() => void cancel(upload)}>{mediaMessages.cancel}</button>}
            </span>
          </li>
        ))}
      </ul>
      <input ref={file} type="file" hidden accept="video/mp4,video/webm,video/quicktime,.mov" onChange={(event) => {
        const chosen = event.target.files?.[0];
        event.target.value = '';
        const upload = resuming.current;
        if (!chosen || !upload) return;
        // The server knows the name and size, not the bytes: both must match, and complete checks the file.
        if (chosen.size !== upload.size || baseName(chosen.name) !== upload.name) { setProblem(mediaMessages.notTheSameFile(upload.name)); return; }
        setProblem(undefined);
        queue.resumeUpload(chosen, upload.id);
      }} />
    </section>
  );
}

function Uploads({ rows, onPause, onResume, onCancel, onClear }: { rows: UploadRow[]; onPause: (key: number) => void; onResume: (key: number) => void; onCancel: (key: number) => void; onClear: () => void }) {
  const finished = rows.some((row) => row.state === 'done' || row.state === 'cancelled');
  return (
    <section className="media-uploads" aria-labelledby="media-uploads-heading">
      <div className="media-uploads-head">
        <h2 id="media-uploads-heading">{mediaMessages.uploads}</h2>
        {finished && <button type="button" className="link-action" onClick={onClear}>{mediaMessages.clearFinished}</button>}
      </div>
      <ul>
        {rows.map((row) => {
          const percent = row.size ? Math.min(100, Math.round((row.sent / row.size) * 100)) : 0;
          const left = row.bytesPerSecond > 0 ? (row.size - row.sent) / row.bytesPerSecond : undefined;
          const status = row.state === 'done' ? (row.duplicate ? mediaMessages.alreadyInLibrary : mediaMessages.uploaded)
            : row.state === 'sending' ? [mediaMessages.sending(row.sent, row.size), row.kind === 'video' && row.bytesPerSecond > 0 ? mediaMessages.speed(row.bytesPerSecond) : '',
              left !== undefined && row.kind === 'video' ? mediaMessages.timeLeft(left) : ''].filter(Boolean).join(' · ')
            : row.state === 'paused' ? mediaMessages.paused : row.state === 'waiting' ? mediaMessages.waiting : row.state === 'cancelled' ? mediaMessages.cancelled : row.problem;
          return (
            <li key={row.key} data-state={row.state}>
              <span className="media-upload-name">{row.name}</span>
              <progress max={100} value={row.state === 'done' ? 100 : percent} aria-label={row.name} />
              <span className="media-upload-status" role={row.state === 'failed' ? 'alert' : undefined}>{status}</span>
              <span className="button-row">
                {row.kind === 'video' && (row.state === 'sending' || row.state === 'waiting') && <button type="button" onClick={() => onPause(row.key)} aria-label={mediaMessages.pauseName(row.name)}>{mediaMessages.pause}</button>}
                {(row.state === 'paused' || row.state === 'failed') && <button type="button" onClick={() => onResume(row.key)} aria-label={mediaMessages.resumeName(row.name)}>{mediaMessages.resume}</button>}
                {row.state !== 'done' && row.state !== 'cancelled' && <button type="button" onClick={() => onCancel(row.key)} aria-label={mediaMessages.cancelName(row.name)}>{mediaMessages.cancel}</button>}
              </span>
            </li>
          );
        })}
      </ul>
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

function Library({ search, go, onChanged }: { search: MediaSearch; go: (to: MediaView, change?: Partial<MediaSearch>, replace?: boolean) => void; onChanged: () => void }) {
  const filter = { q: search.q, kind: search.kind, usage: search.usage, sort: search.sort };
  const list = useMediaList(filter);
  // The size of what no article uses, for the line that leads to the Unused page.
  const unused = useQuery({ queryKey: ['kb-media', 'unused-summary'], queryFn: ({ signal }) => mediaApi.list({ usage: 'unused', sort: 'largest' }, signal) });
  const summary = unused.data?.summary;
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
  return (
    <div className="media-layout" data-open={search.item ? 'true' : undefined}>
      <section className="media-main" aria-labelledby="media-list-heading">
        <h2 id="media-list-heading" className="sr-only">{mediaMessages.heading}</h2>
        {summary && summary.count + summary.kept_count > 0 && <p className="media-summary">
          {mediaMessages.unusedSummary(summary.count + summary.kept_count, summary.bytes + summary.kept_bytes)}{' · '}
          <RouterLink href={viewPath.unused} onFollow={() => go('unused')}>{mediaMessages.review}</RouterLink>
        </p>}
        <div className="kb-filters media-filters" role="search">
          <label className="search-field kb-search">
            <SearchIcon />
            <span className="sr-only">{mediaMessages.search}</span>
            <input type="search" value={text} placeholder={mediaMessages.search} maxLength={200} aria-describedby="media-search-hint" onChange={(event) => type(event.target.value)} />
          </label>
          <fieldset className="segmented">
            <legend className="sr-only">{mediaMessages.kind}</legend>
            {([[undefined, mediaMessages.anyKind], ['image', mediaMessages.images], ['video', mediaMessages.videos]] as const).map(([value, label]) => (
              <label key={label}><input type="radio" name="media-kind" checked={search.kind === value} onChange={() => go('library', { kind: value })} />{label}</label>
            ))}
          </fieldset>
          <label className="media-select">{mediaMessages.usage}
            <select value={search.usage ?? ''} onChange={(event) => go('library', { usage: (event.target.value || undefined) as MediaUsage | undefined })}>
              <option value="">{mediaMessages.anyUsage}</option>
              <option value="in_use">{mediaMessages.inUse}</option>
              <option value="unused">{mediaMessages.unused}</option>
              <option value="missing_alt">{mediaMessages.missingAlt}</option>
            </select>
          </label>
          <label className="media-select">{mediaMessages.sort}
            <select value={search.sort ?? 'newest'} onChange={(event) => go('library', { sort: event.target.value === 'newest' ? undefined : event.target.value as MediaSort })}>
              <option value="newest">{mediaMessages.newest}</option>
              <option value="oldest">{mediaMessages.oldest}</option>
              <option value="name">{mediaMessages.name}</option>
              <option value="largest">{mediaMessages.largest}</option>
            </select>
          </label>
        </div>
        <p id="media-search-hint" className="kb-hint">{mediaMessages.searchHint}</p>
        {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}{' '}
          <button type="button" className="link-action" onClick={() => void list.refetch()}>{mediaMessages.retry}</button></p>}
        {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
        {list.isSuccess && items.length === 0 && <p className="empty-copy">{filtered ? mediaMessages.noMatches : mediaMessages.empty}</p>}
        {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.heading} aria-busy={list.isFetching && !list.isFetchingNextPage}>
          {items.map((item) => <MediaCard key={item.id} item={item} open={item.id === search.item}
            onOpen={(chosen) => go('library', { item: chosen.id === search.item ? undefined : chosen.id })} />)}
        </ul>}
        {list.hasNextPage && <button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {list.isFetchingNextPage ? mediaMessages.loading : mediaMessages.loadMore}</button>}
      </section>
      {search.item && <MediaDetail key={search.item} id={search.item} onChanged={onChanged} onClose={() => go('library', { item: undefined })} />}
    </div>
  );
}

// useSelection ticks items of a list; a Shift-click ticks the range from the last one ticked.
function useSelection(items: KBMedia[]) {
  const [ids, setIds] = useState<Set<string>>(new Set());
  const last = useRef<string | undefined>(undefined);
  const toggle = (item: KBMedia, range: boolean) => {
    // Read now: the updater runs later, when last already names this item.
    const anchor = range ? last.current : undefined;
    setIds((before) => {
      const next = new Set(before);
      const from = anchor ? items.findIndex((each) => each.id === anchor) : -1;
      const to = items.findIndex((each) => each.id === item.id);
      if (from >= 0 && to >= 0) {
        const on = !before.has(item.id);
        for (const each of items.slice(Math.min(from, to), Math.max(from, to) + 1)) { if (on) next.add(each.id); else next.delete(each.id); }
      } else if (next.has(item.id)) next.delete(item.id);
      else next.add(item.id);
      return next;
    });
    last.current = item.id;
  };
  const chosen = items.filter((item) => ids.has(item.id));
  return { ids, chosen, toggle, all: () => setIds(new Set(items.map((item) => item.id))), clear: () => setIds(new Set()) };
}

interface Confirming { all: boolean; count: number; bytes: number }

function Unused({ onChanged }: { onChanged: () => void }) {
  // The moment the page opened: "Move all unused" leaves out what was uploaded after it.
  const [opened] = useState(() => new Date().toISOString());
  const list = useMediaList({ usage: 'unused', sort: 'largest' });
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  const summary = list.data?.pages[0]?.summary;
  const selection = useSelection(items);
  const [includeKept, setIncludeKept] = useState(false);
  const [confirming, setConfirming] = useState<Confirming>();
  const [done, setDone] = useState<{ text: string; batch?: string }>();
  const [problem, setProblem] = useState<string>();
  const [now] = useState(() => new Date());
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timezone = me.data?.timezone ?? 'UTC';
  const move = async () => {
    if (!confirming) return;
    setProblem(undefined);
    try {
      if (confirming.all) {
        let batch: string | undefined;
        let moved = 0;
        try {
          for (let more = true; more;) {
            const answer = await mediaApi.trashUnused({ before: opened, include_kept: includeKept, ...(batch ? { batch_id: batch } : {}) });
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
  const allCount = summary ? summary.count + (includeKept ? summary.kept_count : 0) : 0;
  const allBytes = summary ? summary.bytes + (includeKept ? summary.kept_bytes : 0) : 0;
  return (
    <section className="media-main" aria-labelledby="media-unused-heading">
      <h2 id="media-unused-heading" className="sr-only">{mediaMessages.unusedHeading}</h2>
      {summary && <p className="media-summary">
        <strong>{mediaMessages.canBeFreed(summary.count, summary.bytes)}</strong>
        {summary.kept_count > 0 && <span className="muted-copy"> · {mediaMessages.keptApart(summary.kept_count, summary.kept_bytes)}</span>}
      </p>}
      {done && <p role="status" className="media-toast">{done.text}{' '}
        {done.batch && <button type="button" className="link-action" onClick={() => void undo()}>{mediaMessages.undo}</button>}</p>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {items.length > 0 && <div className="media-selectbar">
        <span>{mediaMessages.selected(selection.chosen.length)}</span>
        <button type="button" className="link-action" onClick={selection.all}>{mediaMessages.selectAll(items.length)}</button>
        {selection.chosen.length > 0 && <button type="button" className="link-action" onClick={selection.clear}>{mediaMessages.clearSelection}</button>}
        <span className="button-row">
          <button type="button" disabled={selection.chosen.length === 0} onClick={() => setConfirming({ all: false, count: selection.chosen.length, bytes: chosenBytes })}>{mediaMessages.moveSelected}</button>
          <button type="button" className="danger-action" disabled={allCount === 0} onClick={() => setConfirming({ all: true, count: allCount, bytes: allBytes })}>{mediaMessages.moveAllUnused}</button>
        </span>
        {summary && summary.kept_count > 0 && <label className="checkbox-row">
          <input type="checkbox" checked={includeKept} onChange={(event) => setIncludeKept(event.target.checked)} />{mediaMessages.includeKept(summary.kept_count)}</label>}
      </div>}
      {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}{' '}
        <button type="button" className="link-action" onClick={() => void list.refetch()}>{mediaMessages.retry}</button></p>}
      {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
      {list.isSuccess && items.length === 0 && <p className="empty-copy">{mediaMessages.noUnused}</p>}
      {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.unusedHeading}>
        {items.map((item) => <SelectCard key={item.id} item={item} checked={selection.ids.has(item.id)} onToggle={selection.toggle}
          extra={item.purge_at ? mediaMessages.purgesOn(formatDate(item.purge_at, timezone, now)) : item.kept ? mediaMessages.kept : undefined} />)}
      </ul>}
      {list.hasNextPage && <button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
        {list.isFetchingNextPage ? mediaMessages.loading : mediaMessages.loadMore}</button>}
      {confirming && <ConfirmDialog heading={mediaMessages.confirmHeading} confirm={mediaMessages.confirmMove} onConfirm={move} onCancel={() => setConfirming(undefined)}>
        <p>{confirming.all ? mediaMessages.confirmAll(confirming.count, confirming.bytes) : mediaMessages.confirmCount(confirming.count, confirming.bytes)}</p>
        {confirming.all && !includeKept && <p>{mediaMessages.confirmKeptSkipped}</p>}
        {!confirming.all && <ConfirmUsage items={selection.chosen} />}
        <p className="muted-copy">{mediaMessages.confirmSpace}</p>
      </ConfirmDialog>}
    </section>
  );
}

// ConfirmUsage counts the chosen items that only deleted articles or older versions still name.
function ConfirmUsage({ items }: { items: KBMedia[] }) {
  const labels = items.map(usageLabel).filter((label) => label && label !== mediaMessages.notUsed);
  return labels.length > 0 ? <p>{labels.length} of them: {Array.from(new Set(labels)).join('; ')}.</p> : null;
}

function ConfirmDialog({ heading, confirm, danger = false, children, onConfirm, onCancel }: { heading: string; confirm: string; danger?: boolean; children: ReactNode; onConfirm: () => Promise<void>; onCancel: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  // One press sends once: the button waits for the answer.
  const [busy, setBusy] = useState(false);
  useReturnFocus();
  const first = useRef<HTMLButtonElement>(null);
  useEffect(() => { first.current?.focus(); }, []);
  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <div className="dialog" role="alertdialog" aria-modal="true" aria-labelledby="media-confirm-heading" ref={root} onKeyDown={dialogKeys(root, onCancel)}>
        <h2 id="media-confirm-heading">{heading}</h2>
        {children}
        <div className="button-row">
          <button type="button" ref={first} onClick={onCancel}>{mediaMessages.cancel}</button>
          <button type="button" className={danger ? 'danger-action' : 'primary-action'} disabled={busy} aria-busy={busy}
            onClick={() => { setBusy(true); void onConfirm().finally(() => setBusy(false)); }}>{confirm}</button>
        </div>
      </div>
    </div>
  );
}

// LostIn names what deleting forever leaves without its media: deleted articles and older versions
// that still show an item then say "Media no longer exists".
function LostIn({ items }: { items: KBMedia[] }) {
  const trashed = items.reduce((sum, item) => sum + (item.usage?.trashed_articles ?? 0), 0);
  const older = items.reduce((sum, item) => sum + (item.usage?.older_versions ?? 0), 0);
  return trashed + older > 0 ? <p>{mediaMessages.lostIn(trashed, older)}</p> : null;
}

function Trash({ onChanged }: { onChanged: () => void }) {
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
    <section className="media-main" aria-labelledby="media-trash-heading">
      <h2 id="media-trash-heading" className="sr-only">{mediaMessages.trashHeading}</h2>
      {done && <p role="status" className="media-toast">{done}</p>}
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      {items.length > 0 && <div className="media-selectbar">
        <span>{mediaMessages.selected(selection.chosen.length)}</span>
        <button type="button" className="link-action" onClick={selection.all}>{mediaMessages.selectAll(items.length)}</button>
        <span className="button-row">
          <button type="button" disabled={selection.chosen.length === 0} onClick={() => void run('restore')}>{mediaMessages.restoreSelected}</button>
          <button type="button" className="danger-action" disabled={!admin || selection.chosen.length === 0} aria-describedby={admin ? undefined : 'media-admins-only'}
            onClick={() => setDeleting(true)}>{mediaMessages.deleteForever}</button>
        </span>
        {!admin && <span id="media-admins-only" className="kb-hint">{mediaMessages.adminsOnly}</span>}
      </div>}
      {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
      {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}</p>}
      {list.isSuccess && items.length === 0 && <p className="empty-copy">{mediaMessages.trashEmpty}</p>}
      {items.length > 0 && <ul className="media-grid" aria-label={mediaMessages.trashHeading}>
        {items.map((item) => <SelectCard key={item.id} item={item} checked={selection.ids.has(item.id)} onToggle={selection.toggle}
          extra={item.deleted_at ? mediaMessages.trashedOn(formatDate(item.deleted_at, timezone, now)) : undefined} />)}
      </ul>}
      {list.hasNextPage && <button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{mediaMessages.loadMore}</button>}
      {deleting && <ConfirmDialog heading={mediaMessages.deleteForeverHeading} confirm={mediaMessages.deleteForeverConfirm(selection.chosen.length)} danger
        onConfirm={() => run('purge')} onCancel={() => setDeleting(false)}>
        <p>{mediaMessages.deleteForeverBody(selection.chosen.length, bytes)}</p>
        <LostIn items={selection.chosen} />
        <ul className="media-articles">{selection.chosen.slice(0, 10).map((item) => <li key={item.id}>{mediaName(item)} · {formatBytes(item.size)}</li>)}</ul>
      </ConfirmDialog>}
    </section>
  );
}

