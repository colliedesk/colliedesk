import { useEffect, useId, useRef, useState } from 'react';
import type { FormEvent, KeyboardEvent, RefObject } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChangedElsewhereError } from '../../api/client';
import { formatBytes } from '../../lib/format-bytes';
import { RouterLink } from '../../ui/router-link';
import { Icon } from './editor/icons';
import { KBDialog } from './kb-ui';
import { mediaApi } from './media-api';
import type { KBMedia, KBMediaDetails } from './media-api';
import { embedFor, mediaName } from './media-grid';
import { mediaMessages } from './media-messages';
import { reason } from './kb-tree';
import { posterFrom } from './video-poster';

// MediaDetail is the panel of one library item: its preview, what can be done to it, then three tabs
// (Details: name, alt text or a video's title, kept; Used in: the articles; File: the facts and a video's
// poster) and Save at its foot. Each tab fits the panel, so nothing in it is reached by scrolling. Without
// an item it says what it is for: the column stays, and opening an item never moves the grid. On a phone
// it is a sheet over the grid, which keeps its place.
export function MediaDetail({ id, onChanged, onClose }: { id?: string; onChanged: () => void; onClose: () => void }) {
  if (!id) {
    return <aside className="media-detail" data-empty="true" aria-label={mediaMessages.details}><p className="media-detail-none">{mediaMessages.noneSelected}</p></aside>;
  }
  return <OpenDetail key={id} id={id} onChanged={onChanged} onClose={onClose} />;
}

function OpenDetail({ id, onChanged, onClose }: { id: string; onChanged: () => void; onClose: () => void }) {
  const details = useQuery({ queryKey: ['kb-media-details', id], queryFn: () => mediaApi.details(id) });
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); }, [id]);
  const refresh = () => { void details.refetch(); onChanged(); };
  // What the last change said, with an Undo when it has one. It floats over the panel's foot, so it moves nothing.
  const [notice, setNotice] = useState<{ text: string; undo?: () => void }>();
  const name = details.data ? mediaName(details.data) : mediaMessages.details;
  return (
    <>
      <div className="media-detail-scrim" aria-hidden="true" onClick={onClose} />
      <aside className="media-detail" aria-labelledby="media-detail-heading" data-open="true" data-modal="mobile"
        onKeyDown={(event) => { if (event.key === 'Escape' && !(event.target instanceof HTMLVideoElement) && !event.defaultPrevented) onClose(); }}>
        <span className="kb-grab" aria-hidden="true" />
        <header className="media-detail-head">
          <h2 id="media-detail-heading" tabIndex={-1} ref={heading} title={name}>{name}</h2>
          <button type="button" className="icon-button" onClick={onClose} aria-label={mediaMessages.closeDetails}><Icon name="close" /></button>
        </header>
        {details.isPending && <p role="status" className="empty-copy media-detail-pad">{mediaMessages.loading}</p>}
        {details.isError && <p role="alert" className="error-copy media-detail-pad">{mediaMessages.listFailed}{' '}
          <button type="button" className="link-action" onClick={() => void details.refetch()}>{mediaMessages.retry}</button></p>}
        {details.data && <DetailBody key={details.data.id} item={details.data} onChanged={refresh} onNotice={setNotice} onTrashed={() => { onChanged(); onClose(); }} />}
        {notice && <p role="status" className="media-notice">{notice.text}
          {notice.undo && <button type="button" onClick={notice.undo}>{mediaMessages.undo}</button>}
          <button type="button" className="media-notice-close" aria-label={mediaMessages.dismiss} onClick={() => setNotice(undefined)}><Icon name="close" /></button></p>}
      </aside>
    </>
  );
}

type Notify = (notice: { text: string; undo?: () => void } | undefined) => void;
type Tab = 'details' | 'used' | 'file';

function DetailBody({ item, onChanged, onNotice, onTrashed }: { item: KBMediaDetails; onChanged: () => void; onNotice: Notify; onTrashed: () => void }) {
  const id = useId();
  const video = item.kind === 'video';
  const player = useRef<HTMLVideoElement>(null);
  const [tab, setTab] = useState<Tab>('details');
  const [name, setName] = useState(item.name);
  const [alt, setAlt] = useState(item.alt);
  const [kept, setKept] = useState(item.kept);
  // A new version of the item (a save here, a poster drawn after an upload, a teammate's edit) refreshes the
  // fields the person has not touched; what they are typing stays.
  const shown = useRef(item);
  useEffect(() => {
    const before = shown.current;
    shown.current = item;
    if (before === item) return;
    setName((now) => (now === before.name ? item.name : now));
    setAlt((now) => (now === before.alt ? item.alt : now));
    setKept((now) => (now === before.kept ? item.kept : now));
  }, [item]);
  const [state, setState] = useState<{ saving?: boolean; problem?: string }>({});
  const changed = name !== item.name || alt !== item.alt || kept !== item.kept;
  const discard = () => { setName(item.name); setAlt(item.alt); setKept(item.kept); setState({}); };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!changed || state.saving) return;
    setState({ saving: true });
    const send = (version: string) => mediaApi.update(item.id, {
      updated_at: version,
      ...(name !== item.name ? { name } : {}), ...(alt !== item.alt ? { alt } : {}), ...(kept !== item.kept ? { kept } : {}),
    });
    try {
      try {
        await send(item.updated_at);
      } catch (error) {
        // A version saved meanwhile that left these fields as they were (a poster drawn after the upload) is no
        // conflict for them: they are sent again over it. One that changed them is, and says so.
        if (!(error instanceof ChangedElsewhereError)) throw error;
        const fresh = await mediaApi.details(item.id);
        if (fresh.name !== item.name || fresh.alt !== item.alt || fresh.kept !== item.kept) throw error;
        await send(fresh.updated_at);
      }
      setState({});
      onNotice({ text: mediaMessages.saved });
      onChanged();
    } catch (error) {
      setState({ problem: reason(error, mediaMessages.saveFailed) });
    }
  };
  const live = item.articles.filter((article) => !article.in_trash && article.current).length;
  const tabs: [Tab, string][] = [['details', mediaMessages.details], ['used', mediaMessages.usedInCount(live)], ['file', mediaMessages.facts]];
  // Arrows move between the tabs (one Tab stop), as in a tab list.
  const tabKeys = (event: KeyboardEvent<HTMLButtonElement>, at: number) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    const next = tabs[(at + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length]![0];
    setTab(next);
    document.getElementById(`${id}-${next}-tab`)?.focus();
  };
  return (
    <form className="media-detail-body" onSubmit={(event) => void save(event)}>
      <div className="media-detail-main">
        <div className="media-detail-preview">
          {video
            ? <video ref={player} controls preload="metadata" src={item.url} poster={item.poster_url ?? undefined} />
            : <img src={item.url} alt={item.alt} />}
        </div>
        <Actions item={item} onChanged={onChanged} onNotice={onNotice} onTrashed={onTrashed} />
        <div className="media-tabs" role="tablist" aria-label={mediaMessages.itemTabs}>
          {tabs.map(([name_, label], at) => (
            <button key={name_} type="button" role="tab" id={`${id}-${name_}-tab`} aria-selected={tab === name_} aria-controls={`${id}-${name_}`} tabIndex={tab === name_ ? 0 : -1}
              onClick={() => setTab(name_)} onKeyDown={(event) => tabKeys(event, at)}>{label}</button>
          ))}
        </div>
        <div className="media-tabpanel" role="tabpanel" id={`${id}-details`} aria-labelledby={`${id}-details-tab`} hidden={tab !== 'details'}>
          <label className="field">{mediaMessages.nameLabel}
            <input value={name} maxLength={200} onChange={(event) => { setName(event.target.value); setState({}); }} />
          </label>
          <label className="field">{video ? mediaMessages.titleLabel : mediaMessages.altLabel}
            <textarea value={alt} maxLength={500} rows={2} aria-describedby={`${id}-alt-help`} onChange={(event) => { setAlt(event.target.value); setState({}); }} />
          </label>
          <p id={`${id}-alt-help`} className="kb-hint">{video ? mediaMessages.titleHelp : mediaMessages.altHelp}</p>
          <label className="media-keep">
            <input type="checkbox" checked={kept} aria-describedby={`${id}-keep-help`} onChange={(event) => { setKept(event.target.checked); setState({}); }} />
            <span>{mediaMessages.keep}</span>
            <span id={`${id}-keep-help`} className="kb-hint">{live > 0 ? mediaMessages.keepHelpInUse : mediaMessages.keepHelp}</span>
          </label>
        </div>
        <div className="media-tabpanel" role="tabpanel" id={`${id}-used`} aria-labelledby={`${id}-used-tab`} hidden={tab !== 'used'}>
          <Usage item={item} />
        </div>
        <div className="media-tabpanel" role="tabpanel" id={`${id}-file`} aria-labelledby={`${id}-file-tab`} hidden={tab !== 'file'}>
          <dl className="media-facts">
            <dt>{mediaMessages.factType}</dt><dd>{item.content_type}{item.codecs.length > 0 ? ` (${item.codecs.join(', ')})` : ''}</dd>
            {item.width !== null && item.height !== null && <><dt>{mediaMessages.factDimensions}</dt><dd>{mediaMessages.dimensions(item.width, item.height)}</dd></>}
            {item.duration_ms !== null && <><dt>{mediaMessages.factLength}</dt><dd>{mediaMessages.duration(item.duration_ms)}</dd></>}
            <dt>{mediaMessages.factSize}</dt><dd>{formatBytes(item.size)}</dd>
          </dl>
          {video && <Poster item={item} player={player} onSaved={() => { onNotice({ text: mediaMessages.posterSaved }); onChanged(); }} />}
        </div>
      </div>
      <footer className="media-detail-foot">
        <span className="media-save-state" role={state.problem ? 'alert' : undefined}>{state.problem ?? (changed ? mediaMessages.unsavedChanges : '')}</span>
        <button type="button" disabled={!changed || state.saving} onClick={discard}>{mediaMessages.discard}</button>
        <button type="submit" className="primary-action" disabled={!changed || state.saving}>{mediaMessages.save}</button>
      </footer>
    </form>
  );
}

function Poster({ item, player, onSaved }: { item: KBMediaDetails; player: RefObject<HTMLVideoElement | null>; onSaved: () => void }) {
  const [state, setState] = useState<{ busy?: boolean; problem?: string }>({});
  const file = useRef<HTMLInputElement>(null);
  const send = async (image: Promise<Blob> | Blob, failed: string) => {
    setState({ busy: true });
    try {
      await mediaApi.setPoster(item.id, await image);
      setState({});
      onSaved();
    } catch (error) {
      setState({ problem: reason(error, failed) });
    }
  };
  const useFrame = () => {
    const current = player.current;
    if (!current) return;
    // A frame is there to draw once the player holds the current one.
    if (current.readyState < 2) { setState({ problem: mediaMessages.frameNotReady }); return; }
    current.pause();
    void send(posterFrom(current), mediaMessages.frameFailed);
  };
  return (
    <section className="media-poster" aria-labelledby="media-poster">
      <h3 id="media-poster">{mediaMessages.poster}</h3>
      <p className="kb-hint">{item.poster_url ? mediaMessages.posterHelp : mediaMessages.noPoster} {mediaMessages.chooseFrameHelp}</p>
      <div className="button-row">
        <button type="button" disabled={state.busy} onClick={useFrame}>{mediaMessages.useThisFrame}</button>
        <button type="button" disabled={state.busy} onClick={() => file.current?.click()}>{mediaMessages.uploadPoster}</button>
        <input ref={file} type="file" hidden accept="image/png,image/jpeg,image/gif,image/webp"
          onChange={(event) => { const chosen = event.target.files?.[0]; event.target.value = ''; if (chosen) void send(chosen, mediaMessages.posterFailed); }} />
      </div>
      {state.problem && <p role="alert" className="error-copy">{state.problem}</p>}
    </section>
  );
}

function Usage({ item }: { item: KBMediaDetails }) {
  return item.articles.length === 0
    ? <p className="muted-copy">{mediaMessages.notUsedAnywhere}</p>
    : <ul className="media-used">
        {item.articles.map((article) => (
          <li key={article.id}>
            <RouterLink href={`/knowledge/articles/${article.id}`}>
              <span className="media-used-title">{article.title}</span>
              <span className="kb-hint">#{article.number}{article.in_trash ? ` · ${mediaMessages.inTrash}` : ''}{article.current ? '' : ` · ${mediaMessages.olderVersion}`}</span>
              <span className="kb-dot-status" data-status={article.status}>{article.status === 'published' ? mediaMessages.published : mediaMessages.draft}</span>
            </RouterLink>
          </li>
        ))}
      </ul>;
}

function Actions({ item, onChanged, onNotice, onTrashed }: { item: KBMediaDetails; onChanged: () => void; onNotice: Notify; onTrashed: () => void }) {
  const [problem, setProblem] = useState<string>();
  const [replacing, setReplacing] = useState(false);
  const live = item.articles.filter((article) => !article.in_trash && article.current);
  const trash = async () => {
    setProblem(undefined);
    try {
      const { results } = await mediaApi.moveToTrash({ ids: [item.id] });
      if (results[0]?.status === 'in_use') { setProblem(mediaMessages.inUseRefused); return; }
      onTrashed();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  const replace = async (other: KBMedia) => {
    setProblem(undefined);
    try {
      const done = await mediaApi.replace(item.id, other.id);
      setReplacing(false);
      onNotice({ text: mediaMessages.replaced(done.article_ids.length), undo: () => void undoReplace(done.id) });
      onChanged();
    } catch (error) {
      setReplacing(false);
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  const undoReplace = async (replacement: string) => {
    try {
      const done = await mediaApi.undoReplacement(replacement);
      onNotice({ text: done.skipped_article_ids.length ? mediaMessages.undoneSkipped(done.skipped_article_ids.length) : mediaMessages.undone });
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  return (
    <div className="media-actions">
      <div className="media-action-row">
        <button type="button" aria-label={mediaMessages.copyEmbed} title={mediaMessages.copyEmbed} onClick={() => void navigator.clipboard.writeText(embedFor(item)).then(() => onNotice({ text: mediaMessages.copied }))}><Icon name="copy" />{mediaMessages.embed}</button>
        <a className="button-link" href={`/api/v1/kb/media/${item.id}?download=1`} download><Icon name="download" />{mediaMessages.download}</a>
        <button type="button" aria-haspopup="dialog" onClick={() => setReplacing(true)}><Icon name="replace" />{mediaMessages.replace}</button>
        <button type="button" className="danger-action media-trash-button" aria-label={mediaMessages.moveToTrash} data-tip={mediaMessages.moveToTrash} onClick={() => void trash()}><Icon name="trash" /></button>
      </div>
      {problem && <div role="alert" className="error-copy media-problem">
        <p>{problem}</p>
        {problem === mediaMessages.inUseRefused && <ul className="media-articles">
          {live.map((article) => <li key={article.id}><RouterLink href={`/knowledge/articles/${article.id}`}>{article.title}</RouterLink></li>)}
        </ul>}
      </div>}
      {replacing && <ReplacePicker item={item} onPick={(other) => void replace(other)} onClose={() => setReplacing(false)} />}
    </div>
  );
}

// ReplacePicker lists the other items of the same kind, the newest first, with a search, in a dialog.
function ReplacePicker({ item, onPick, onClose }: { item: KBMedia; onPick: (other: KBMedia) => void; onClose: () => void }) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), 250); return () => window.clearTimeout(timer); }, [text]);
  const others = useQuery({ queryKey: ['kb-media', { kind: item.kind, q, for: 'replace' }], queryFn: ({ signal }) => mediaApi.list({ kind: item.kind, q: q || undefined }, signal) });
  const items = (others.data?.items ?? []).filter((other) => other.id !== item.id);
  return (
    <KBDialog title={mediaMessages.replaceHeading(item.kind)} phone="full" closeLabel={mediaMessages.close} className="media-replace" onClose={onClose}>
      <p className="kb-hint">{mediaMessages.replaceHelp}</p>
      <input type="search" value={text} placeholder={mediaMessages.search} aria-label={mediaMessages.search} onChange={(event) => setText(event.target.value)} />
      {others.isSuccess && items.length === 0 && <p className="muted-copy">{mediaMessages.noMatches}</p>}
      <ul className="media-replace-list">
        {items.map((other) => (
          <li key={other.id}>
            <button type="button" onClick={() => onPick(other)} aria-label={mediaMessages.replaceWith(mediaName(other))}>
              {other.thumbnail_url ? <img src={other.thumbnail_url} alt="" loading="lazy" /> : <span className="media-picture-empty" aria-hidden="true" />}
              <span>{mediaName(other)}</span>
            </button>
          </li>
        ))}
      </ul>
    </KBDialog>
  );
}
