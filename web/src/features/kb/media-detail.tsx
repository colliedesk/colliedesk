import { useEffect, useRef, useState } from 'react';
import type { FormEvent, RefObject } from 'react';
import { useQuery } from '@tanstack/react-query';
import { formatBytes } from '../../lib/format-bytes';
import { RouterLink } from '../../ui/router-link';
import { mediaApi } from './media-api';
import type { KBMedia, KBMediaDetails } from './media-api';
import { mediaName } from './media-grid';
import { mediaMessages } from './media-messages';
import { reason } from './kb-tree';

// MediaDetail is the panel of one library item: what it is, where it is used, its name and alt text
// (a video's title), a video's poster, and what can be done to it.
export function MediaDetail({ id, onChanged, onClose }: { id: string; onChanged: () => void; onClose: () => void }) {
  const details = useQuery({ queryKey: ['kb-media-details', id], queryFn: () => mediaApi.details(id) });
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); }, [id]);
  const refresh = () => { void details.refetch(); onChanged(); };
  return (
    <aside className="media-detail" aria-labelledby="media-detail-heading" onKeyDown={(event) => { if (event.key === 'Escape' && !(event.target instanceof HTMLVideoElement)) onClose(); }}>
      <div className="media-detail-head">
        <h2 id="media-detail-heading" tabIndex={-1} ref={heading}>{details.data ? mediaName(details.data) : mediaMessages.details}</h2>
        <button type="button" onClick={onClose} aria-label={mediaMessages.closeDetails}>{mediaMessages.close}</button>
      </div>
      {details.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
      {details.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}{' '}
        <button type="button" className="link-action" onClick={() => void details.refetch()}>{mediaMessages.retry}</button></p>}
      {details.data && <DetailBody key={details.data.updated_at} item={details.data} onChanged={refresh} onTrashed={() => { onChanged(); onClose(); }} />}
    </aside>
  );
}

function DetailBody({ item, onChanged, onTrashed }: { item: KBMediaDetails; onChanged: () => void; onTrashed: () => void }) {
  const video = item.kind === 'video';
  const player = useRef<HTMLVideoElement>(null);
  return (
    <div className="media-detail-body">
      <div className="media-detail-preview">
        {video
          ? <video ref={player} controls preload="metadata" src={item.url} poster={item.poster_url ?? undefined} />
          : <img src={item.url} alt={item.alt} />}
      </div>
      <MetaForm item={item} onSaved={onChanged} />
      <section aria-labelledby="media-facts">
        <h3 id="media-facts">{mediaMessages.facts}</h3>
        <dl className="media-facts">
          <dt>Type</dt><dd>{item.content_type}{item.codecs.length > 0 ? ` (${item.codecs.join(', ')})` : ''}</dd>
          {item.width !== null && item.height !== null && <><dt>Size</dt><dd>{mediaMessages.dimensions(item.width, item.height)}</dd></>}
          {item.duration_ms !== null && <><dt>Length</dt><dd>{mediaMessages.duration(item.duration_ms)}</dd></>}
          <dt>File</dt><dd>{formatBytes(item.size)}</dd>
        </dl>
      </section>
      {video && <Poster item={item} player={player} onSaved={onChanged} />}
      <Usage item={item} />
      <Actions item={item} onChanged={onChanged} onTrashed={onTrashed} />
    </div>
  );
}

function MetaForm({ item, onSaved }: { item: KBMediaDetails; onSaved: () => void }) {
  const [name, setName] = useState(item.name);
  const [alt, setAlt] = useState(item.alt);
  const [kept, setKept] = useState(item.kept);
  const [state, setState] = useState<{ saving?: boolean; saved?: boolean; problem?: string }>({});
  const changed = name !== item.name || alt !== item.alt || kept !== item.kept;
  const save = async (event: FormEvent) => {
    event.preventDefault();
    setState({ saving: true });
    try {
      await mediaApi.update(item.id, {
        updated_at: item.updated_at,
        ...(name !== item.name ? { name } : {}), ...(alt !== item.alt ? { alt } : {}), ...(kept !== item.kept ? { kept } : {}),
      });
      setState({ saved: true });
      onSaved();
    } catch (error) {
      setState({ problem: reason(error, mediaMessages.saveFailed) });
    }
  };
  const video = item.kind === 'video';
  return (
    <form className="media-form" onSubmit={(event) => void save(event)}>
      <label>{mediaMessages.nameLabel}
        <input value={name} maxLength={200} onChange={(event) => { setName(event.target.value); setState({}); }} />
      </label>
      <label>{video ? mediaMessages.titleLabel : mediaMessages.altLabel}
        <textarea value={alt} maxLength={500} rows={2} aria-describedby="media-alt-help" onChange={(event) => { setAlt(event.target.value); setState({}); }} />
      </label>
      <p id="media-alt-help" className="kb-hint">{video ? mediaMessages.titleHelp : mediaMessages.altHelp}</p>
      <label className="checkbox-row">
        <input type="checkbox" checked={kept} aria-describedby="media-keep-help" onChange={(event) => { setKept(event.target.checked); setState({}); }} />
        {mediaMessages.keep}
      </label>
      <p id="media-keep-help" className="kb-hint">{mediaMessages.keepHelp}</p>
      <div className="button-row">
        <button type="submit" className="primary-action" disabled={!changed || state.saving}>{mediaMessages.save}</button>
        {state.saved && <span role="status" className="muted-copy">{mediaMessages.saved}</span>}
      </div>
      {state.problem && <p role="alert" className="error-copy">{state.problem}</p>}
    </form>
  );
}

// posterFrom draws the frame the player shows into a JPEG of at most 1280 pixels on a side.
async function posterFrom(player: HTMLVideoElement): Promise<Blob> {
  const scale = Math.min(1, 1280 / Math.max(player.videoWidth, player.videoHeight));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(player.videoWidth * scale));
  canvas.height = Math.max(1, Math.round(player.videoHeight * scale));
  const context = canvas.getContext('2d');
  if (!context || player.videoWidth === 0) throw new Error('no frame');
  context.drawImage(player, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) => canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('no frame'))), 'image/jpeg', 0.82));
}

function Poster({ item, player, onSaved }: { item: KBMediaDetails; player: RefObject<HTMLVideoElement | null>; onSaved: () => void }) {
  const [state, setState] = useState<{ busy?: boolean; saved?: boolean; problem?: string }>({});
  const file = useRef<HTMLInputElement>(null);
  const send = async (image: Promise<Blob> | Blob, failed: string) => {
    setState({ busy: true });
    try {
      await mediaApi.setPoster(item.id, await image);
      setState({ saved: true });
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
    <section aria-labelledby="media-poster">
      <h3 id="media-poster">{mediaMessages.poster}</h3>
      <p className="kb-hint">{item.poster_url ? mediaMessages.posterHelp : mediaMessages.noPoster}</p>
      <p className="kb-hint">{mediaMessages.chooseFrameHelp}</p>
      <div className="button-row">
        <button type="button" disabled={state.busy} onClick={useFrame}>{mediaMessages.useThisFrame}</button>
        <button type="button" disabled={state.busy} onClick={() => file.current?.click()}>{mediaMessages.uploadPoster}</button>
        <input ref={file} type="file" hidden accept="image/png,image/jpeg,image/gif,image/webp"
          onChange={(event) => { const chosen = event.target.files?.[0]; event.target.value = ''; if (chosen) void send(chosen, mediaMessages.posterFailed); }} />
        {state.saved && <span role="status" className="muted-copy">{mediaMessages.posterSaved}</span>}
      </div>
      {state.problem && <p role="alert" className="error-copy">{state.problem}</p>}
    </section>
  );
}

function Usage({ item }: { item: KBMediaDetails }) {
  return (
    <section aria-labelledby="media-used-in">
      <h3 id="media-used-in">{mediaMessages.usedInHeading}</h3>
      {item.articles.length === 0
        ? <p className="muted-copy">{mediaMessages.notUsedAnywhere}</p>
        : <ul className="media-articles">
            {item.articles.map((article) => (
              <li key={article.id}>
                <RouterLink href={`/knowledge/articles/${article.id}`}>{article.title}</RouterLink>
                <span className="kb-hint"> #{article.number}{article.in_trash ? ` · ${mediaMessages.inTrash}` : ''}{article.current ? '' : ` · ${mediaMessages.olderVersion}`}</span>
              </li>
            ))}
          </ul>}
    </section>
  );
}

// embedFor is what an article holds to show the item: Markdown for an image, the allowed <video> HTML
// for a video, as a block of its own.
export function embedFor(item: KBMedia) {
  if (item.kind === 'image') return `![${item.alt.replace(/[[\]]/g, '')}](${item.url})`;
  const title = item.alt ? ` title="${item.alt.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"` : '';
  return `<video controls preload="metadata" src="${item.url}"${title}></video>`;
}

function Actions({ item, onChanged, onTrashed }: { item: KBMediaDetails; onChanged: () => void; onTrashed: () => void }) {
  const [message, setMessage] = useState<string>();
  const [problem, setProblem] = useState<string>();
  const [replacing, setReplacing] = useState(false);
  const [undo, setUndo] = useState<string>();
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
      setUndo(done.id);
      setMessage(mediaMessages.replaced(done.article_ids.length));
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  const undoReplace = async () => {
    if (!undo) return;
    try {
      const done = await mediaApi.undoReplacement(undo);
      setUndo(undefined);
      setMessage(done.skipped_article_ids.length ? mediaMessages.undoneSkipped(done.skipped_article_ids.length) : mediaMessages.undone);
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    }
  };
  const [copied, setCopied] = useState(false);
  return (
    <section className="media-actions" aria-label={mediaMessages.details}>
      <div className="button-row">
        <button type="button" onClick={() => void navigator.clipboard.writeText(embedFor(item)).then(() => setCopied(true))}>{mediaMessages.copyEmbed}</button>
        <a className="button-link" href={`/api/v1/kb/media/${item.id}?download=1`} download>{mediaMessages.download}</a>
        <button type="button" aria-expanded={replacing} onClick={() => setReplacing(!replacing)}>{mediaMessages.replace}</button>
        <button type="button" className="danger-action" onClick={() => void trash()}>{mediaMessages.moveToTrash}</button>
        {copied && <span role="status" className="muted-copy">{mediaMessages.copied}</span>}
      </div>
      {message && <p role="status" className="muted-copy">{message}{' '}
        {undo && <button type="button" className="link-action" onClick={() => void undoReplace()}>{mediaMessages.undo}</button>}</p>}
      {problem && <div role="alert" className="error-copy">
        <p>{problem}</p>
        {problem === mediaMessages.inUseRefused && <ul className="media-articles">
          {live.map((article) => <li key={article.id}><RouterLink href={`/knowledge/articles/${article.id}`}>{article.title}</RouterLink></li>)}
        </ul>}
      </div>}
      {replacing && <ReplacePicker item={item} onPick={(other) => void replace(other)} />}
    </section>
  );
}

// ReplacePicker lists the other items of the same kind, the newest first, with a search.
function ReplacePicker({ item, onPick }: { item: KBMedia; onPick: (other: KBMedia) => void }) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), 250); return () => window.clearTimeout(timer); }, [text]);
  const others = useQuery({ queryKey: ['kb-media', { kind: item.kind, q, for: 'replace' }], queryFn: ({ signal }) => mediaApi.list({ kind: item.kind, q: q || undefined }, signal) });
  const items = (others.data?.items ?? []).filter((other) => other.id !== item.id);
  return (
    <div className="media-replace" role="group" aria-labelledby="media-replace-heading">
      <h3 id="media-replace-heading">{mediaMessages.replaceHeading(item.kind)}</h3>
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
    </div>
  );
}
