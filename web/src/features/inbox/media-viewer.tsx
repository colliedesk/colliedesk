import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { CSSProperties, KeyboardEvent, RefObject, TouchEvent } from 'react';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate, stampTitle } from '../../lib/format-date';
import { keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { AudioIcon, ChevronLeftIcon, ImageIcon, ImageOffIcon, ChevronRightIcon, DownloadIcon, ExpandIcon, MaximizeIcon, MinimizeIcon, PauseIcon, PlayIcon, VolumeIcon, VolumeOffIcon, XIcon } from '../../ui/icons';
import { attachmentURL, thumbnailURL } from './media-kind';
import type { GalleryItem } from './media-kind';
import { mediaMessages as m } from './media-messages';
import './media-viewer.css';

// The player and gallery of a conversation's pictures, videos and voice clips (docs/user/inbox.md "Pictures,
// videos and voice clips"). The bytes come from the download route only, which checks the workspace and never
// serves a file as a page; a range request reads a video's metadata and seeks without the whole file.

const RATES = [1, 1.25, 1.5, 2, 0.75];
const SLIDER_KEYS = new Set(['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown']);
const TABBABLE = 'button:not(:disabled):not([tabindex="-1"]), a[href], input:not(:disabled)';

export function formatTime(value: number) {
  const total = Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = String(total % 60).padStart(2, '0');
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, '0')}:${seconds}` : `${minutes}:${seconds}`;
}

type FullScreenVideo = HTMLVideoElement & { webkitEnterFullscreen?: () => void };

interface PlayerProps {
  kind: 'video' | 'audio';
  src: string;
  title: string;
  // lazy keeps preload at none until the player is near the screen: a long timeline asks for nothing it does not show.
  lazy?: boolean;
  onExpand?: () => void;
  // openerId marks the expand button, so the gallery it opens gives the focus back to it.
  openerId?: string;
  onError?: () => void;
}

// MediaPlayer is the one player of video and audio: its own controls (play, seek, time, volume, speed, full screen
// for a video), never the browser's, so it looks and works the same everywhere. Keys on the player: k or Space
// plays and pauses, m mutes, f goes full screen; the sliders take the arrow keys.
export function MediaPlayer({ kind, src, title, lazy = false, onExpand, openerId, onError }: PlayerProps) {
  const root = useRef<HTMLDivElement>(null);
  const media = useRef<HTMLMediaElement | null>(null);
  const [near, setNear] = useState(!lazy);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [rate, setRate] = useState(1);
  const [full, setFull] = useState(false);
  const [waiting, setWaiting] = useState(false);

  useEffect(() => {
    if (near) return;
    const element = root.current;
    if (!element || typeof IntersectionObserver === 'undefined') { setNear(true); return; }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) { setNear(true); observer.disconnect(); }
    }, { rootMargin: '300px' });
    observer.observe(element);
    return () => observer.disconnect();
  }, [near]);

  useEffect(() => {
    const changed = () => setFull(document.fullscreenElement === root.current);
    document.addEventListener('fullscreenchange', changed);
    return () => document.removeEventListener('fullscreenchange', changed);
  }, []);

  // A player taken off the screen (another item in the gallery, the gallery closed) stops playing.
  useEffect(() => {
    const element = media.current;
    return () => { if (element && !element.paused) element.pause(); };
  }, []);

  const toggle = useCallback(() => {
    const element = media.current;
    if (!element) return;
    setNear(true);
    if (element.paused) void element.play()?.catch(() => undefined);
    else element.pause();
  }, []);
  const toggleMute = () => {
    const element = media.current;
    if (!element) return;
    element.muted = !element.muted;
    setMuted(element.muted);
  };
  const setLevel = (level: number) => {
    const element = media.current;
    if (!element) return;
    element.volume = level;
    element.muted = level === 0;
    setVolume(level);
    setMuted(element.muted);
  };
  const nextRate = () => {
    const element = media.current;
    const next = RATES[(RATES.indexOf(rate) + 1) % RATES.length];
    if (element) element.playbackRate = next;
    setRate(next);
  };
  const seek = (to: number) => {
    const element = media.current;
    if (!element) return;
    element.currentTime = to;
    setTime(to);
  };
  const toggleFull = () => {
    if (document.fullscreenElement) { void document.exitFullscreen?.().catch(() => undefined); return; }
    const element = root.current;
    if (element?.requestFullscreen) void element.requestFullscreen().catch(() => undefined);
    else (media.current as FullScreenVideo | null)?.webkitEnterFullscreen?.();
  };

  const keys = (event: KeyboardEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement;
    // A slider moves with the arrows; the gallery around it must not change the item as well.
    if (target instanceof HTMLInputElement && target.type === 'range') {
      if (SLIDER_KEYS.has(event.key)) event.stopPropagation();
      return;
    }
    const onControl = target.tagName === 'BUTTON' || target.tagName === 'A';
    if (event.key === 'k' || (event.key === ' ' && !onControl)) { event.preventDefault(); toggle(); }
    else if (event.key === 'm') { event.preventDefault(); toggleMute(); }
    else if (event.key === 'f' && kind === 'video') { event.preventDefault(); toggleFull(); }
  };

  const read = (element: HTMLMediaElement) => setDuration(Number.isFinite(element.duration) ? element.duration : 0);
  const events = {
    onPlay: () => setPlaying(true),
    onPause: () => setPlaying(false),
    onEnded: () => setPlaying(false),
    onTimeUpdate: () => setTime(media.current?.currentTime ?? 0),
    onLoadedMetadata: () => { if (media.current) read(media.current); },
    onDurationChange: () => { if (media.current) read(media.current); },
    onVolumeChange: () => { if (media.current) { setMuted(media.current.muted); setVolume(media.current.volume); } },
    onRateChange: () => setRate(media.current?.playbackRate ?? 1),
    onWaiting: () => setWaiting(true),
    onPlaying: () => setWaiting(false),
    onCanPlay: () => setWaiting(false),
    onError: () => onError?.(),
  };
  const preload = near ? 'metadata' : 'none';
  const filled = duration > 0 ? `${Math.min(100, (time / duration) * 100)}%` : '0%';
  const level = muted ? 0 : volume;
  return (
    <div ref={root} className={`media-player media-${kind}${playing ? ' is-playing' : ''}${full ? ' is-full' : ''}`} onKeyDown={keys}>
      {kind === 'video'
        ? <div className="media-screen">
          <video ref={(element) => { media.current = element; }} src={src} preload={preload} playsInline aria-label={title} onClick={toggle} {...events} />
          {/* A mark only: a press goes through it to the video, and the Play button below is the control. */}
          {!playing && <span className="media-big-play" aria-hidden="true"><PlayIcon /></span>}
          {waiting && playing && <span className="media-waiting" role="status" aria-label={m.loading} />}
        </div>
        : <audio ref={(element) => { media.current = element; }} src={src} preload={preload} {...events} />}
      <div className="media-controls">
        <button type="button" className="media-control media-toggle" aria-label={playing ? m.pause : m.play} onClick={toggle}>{playing ? <PauseIcon /> : <PlayIcon />}</button>
        <input type="range" className="media-range media-seek" aria-label={m.seek} min={0} max={duration || 0} step="any" value={Math.min(time, duration || 0)}
          disabled={!duration} aria-valuetext={m.seekText(formatTime(time), formatTime(duration))} style={{ '--filled': filled } as CSSProperties}
          onChange={(event) => seek(Number(event.target.value))} />
        <span className="media-time" aria-hidden="true">{`${formatTime(time)} / ${formatTime(duration)}`}</span>
        <span className="media-volume">
          <button type="button" className="media-control" aria-label={muted ? m.unmute : m.mute} onClick={toggleMute}>{muted ? <VolumeOffIcon /> : <VolumeIcon />}</button>
          <input type="range" className="media-range media-level" aria-label={m.volume} min={0} max={1} step={0.05} value={level}
            aria-valuetext={`${Math.round(level * 100)}%`} style={{ '--filled': `${level * 100}%` } as CSSProperties} onChange={(event) => setLevel(Number(event.target.value))} />
        </span>
        <button type="button" className="media-control media-rate" aria-label={m.speed(rate)} onClick={nextRate}>{`${rate}×`}</button>
        {onExpand && <button type="button" className="media-control" aria-label={m.openInGallery(title)} data-media-open={openerId} onClick={onExpand}><ExpandIcon /></button>}
        {kind === 'video' && <button type="button" className="media-control" aria-label={full ? m.exitFullScreen : m.fullScreen} onClick={toggleFull}>{full ? <MinimizeIcon /> : <MaximizeIcon />}</button>}
      </div>
    </div>
  );
}

// MediaGallery shows every picture, video and voice clip of a conversation in timeline order, one at a time:
// arrows (or a swipe) move, Escape closes, the strip below jumps to any of them, and each can be downloaded.
// opener names what opened the gallery (a selector): Safari does not focus a button on a press, so the focus
// goes back to it by name.
export function MediaGallery({ items, start, timezone, onClose, opener }: { items: GalleryItem[]; start: number; timezone: string; onClose: () => void; opener?: string }) {
  const [index, setIndex] = useState(() => Math.max(0, Math.min(items.length - 1, start)));
  const [failed, setFailed] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const strip = useRef<HTMLUListElement>(null);
  const swipe = useRef<number | null>(null);
  const backdrop = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const countId = useId();
  // The gallery is a child of <body> and the rest of the page is inert while it is open: Tab cannot leave it
  // (Safari skips buttons, so a trap at the first and last control is not enough) and a screen reader stays in it.
  // Declared before useReturnFocus, so the page is live again when the focus goes back to the opener.
  useEffect(() => {
    const own = backdrop.current;
    const made = Array.from(document.body.children).filter((element) => element !== own && !element.hasAttribute('inert'));
    made.forEach((element) => element.setAttribute('inert', ''));
    return () => made.forEach((element) => element.removeAttribute('inert'));
  }, []);
  useReturnFocus(opener);
  const total = items.length;
  const item = items[Math.min(index, total - 1)];
  const go = useCallback((to: number) => setIndex(Math.max(0, Math.min(total - 1, to))), [total]);

  // Focus starts on Close, so Tab and Shift+Tab stay inside (dialogKeys wraps from the first and last control).
  // A clip still playing on the page behind stops: the gallery plays one thing at a time.
  useEffect(() => {
    closeButton.current?.focus();
    document.querySelectorAll<HTMLMediaElement>('video, audio').forEach((media) => {
      if (!root.current?.contains(media) && !media.paused) media.pause();
    });
  }, []);
  // The page behind does not scroll while the gallery is open.
  useEffect(() => {
    const before = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = before; };
  }, []);
  useEffect(() => {
    strip.current?.querySelector('[aria-current="true"]')?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [index]);

  // The gallery's keys. Tab moves through its own controls and wraps: Safari skips buttons on Tab, and with the
  // page inert it would otherwise leave for the window, where no key reaches the gallery.
  const handle = (event: { key: string; shiftKey: boolean; preventDefault: () => void }) => {
    if (event.key === 'Escape') { onClose(); return; }
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      go(event.key === 'ArrowLeft' ? index - 1 : event.key === 'ArrowRight' ? index + 1 : event.key === 'Home' ? 0 : total - 1);
      return;
    }
    if (event.key !== 'Tab') return;
    event.preventDefault();
    const controls = Array.from(root.current?.querySelectorAll<HTMLElement>(TABBABLE) ?? []);
    if (controls.length === 0) return;
    const at = controls.indexOf(document.activeElement as HTMLElement);
    const next = at === -1 ? (event.shiftKey ? controls.length - 1 : 0) : (at + (event.shiftKey ? -1 : 1) + controls.length) % controls.length;
    controls[next].focus();
  };
  // A press that lost the focus to the page (Safari's Tab before the fix, a click on nothing) still reaches the gallery.
  const latest = useRef(handle);
  useEffect(() => { latest.current = handle; });
  useEffect(() => {
    const outside = (event: globalThis.KeyboardEvent) => {
      if (root.current?.contains(event.target as Node)) return;
      latest.current(event);
    };
    document.addEventListener('keydown', outside);
    return () => document.removeEventListener('keydown', outside);
  }, []);

  if (!item) return null;
  const keys = (event: KeyboardEvent<HTMLDivElement>) => handle(event);
  const touchStart = (event: TouchEvent) => { swipe.current = event.touches[0]?.clientX ?? null; };
  const touchEnd = (event: TouchEvent) => {
    const from = swipe.current;
    swipe.current = null;
    const to = event.changedTouches[0]?.clientX;
    if (from === null || to === undefined || Math.abs(to - from) < 48) return;
    go(to < from ? index + 1 : index - 1);
  };
  const { file, kind } = item;
  const url = attachmentURL(file.id);
  const broken = failed === file.id;
  return createPortal(
    <div ref={backdrop} className="media-gallery-backdrop" onMouseDown={keepDialogFocus}>
      <div ref={root} className="media-gallery" role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={countId} tabIndex={-1} onKeyDown={keys}>
        <header className="media-gallery-bar">
          <span className="media-gallery-count" id={countId}>{m.position(index + 1, total)}</span>
          <div className="media-gallery-title">
            <h2 id={titleId}>{file.filename}</h2>
            <p>{item.sender} · <time dateTime={item.at} title={stampTitle(item.at, timezone)}>{formatDate(item.at, timezone)}</time> · {formatBytes(file.size)}</p>
          </div>
          <a className="media-gallery-action" href={url} download={file.filename} aria-label={m.download(file.filename)}><DownloadIcon /></a>
          <button type="button" ref={closeButton} className="media-gallery-action" aria-label={m.close} onClick={onClose}><XIcon /></button>
        </header>
        <div className="media-gallery-stage" onClick={(event) => { if (event.target === event.currentTarget) onClose(); }} onTouchStart={touchStart} onTouchEnd={touchEnd}>
          {total > 1 && <button type="button" className="media-gallery-nav media-gallery-prev" aria-label={m.previous} disabled={index === 0} onClick={() => go(index - 1)}><ChevronLeftIcon /></button>}
          <figure key={file.id} className={`media-gallery-figure media-gallery-${kind}`}>
            {broken
              ? <p className="media-gallery-failed">{kind === 'image' && <ImageOffIcon />}{kind === 'image' ? m.notShown : m.notPlayable} <a href={url} download={file.filename}>{m.download(file.filename)}</a></p>
              : kind === 'image'
                ? <img src={url} alt={file.filename} decoding="async" onError={() => setFailed(file.id)} />
                : <>
                  {kind === 'audio' && <span className="media-gallery-audio-mark" aria-hidden="true"><AudioIcon /></span>}
                  <MediaPlayer kind={kind} src={url} title={file.filename} onError={() => setFailed(file.id)} />
                </>}
          </figure>
          {total > 1 && <button type="button" className="media-gallery-nav media-gallery-next" aria-label={m.next} disabled={index === total - 1} onClick={() => go(index + 1)}><ChevronRightIcon /></button>}
        </div>
        {total > 1 && <ul ref={strip} className="media-gallery-strip scroll-x-fade" aria-label={m.strip}>
          {items.map((other, position) => (
            <li key={other.file.id}>
              <button type="button" aria-label={other.file.filename} aria-current={position === index ? 'true' : undefined} onClick={() => go(position)}>
                <StripThumb item={other} near={Math.abs(position - index) <= 3} root={strip} />
              </button>
            </li>
          ))}
        </ul>}
      </div>
    </div>,
    document.body,
  );
}

// StripThumb is one item of the gallery's strip. A picture loads its thumbnail once the strip shows it (an
// IntersectionObserver on the strip; without one, the pictures next to the item shown), then its original if it
// has no thumbnail; until then, and when neither can be drawn, a mark says what the item is.
function StripThumb({ item, near, root }: { item: GalleryItem; near: boolean; root: RefObject<HTMLUListElement | null> }) {
  const box = useRef<HTMLSpanElement>(null);
  const [seen, setSeen] = useState(false);
  const [stage, setStage] = useState<'thumbnail' | 'original' | 'broken'>('thumbnail');
  const picture = item.kind === 'image';
  useEffect(() => {
    const element = box.current;
    if (seen || !picture || !element || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) { setSeen(true); observer.disconnect(); }
    }, { root: root.current, rootMargin: '0px 120px' });
    observer.observe(element);
    return () => observer.disconnect();
  }, [seen, picture, root]);
  const id = item.file.id;
  const show = picture && (seen || near) && stage !== 'broken';
  return (
    <span ref={box} className="media-strip-box">
      {show
        ? <img src={stage === 'original' ? attachmentURL(id) : thumbnailURL(id)} alt="" decoding="async"
          onError={() => setStage(stage === 'thumbnail' ? 'original' : 'broken')} />
        : <span className={`media-strip-mark${stage === 'broken' ? ' is-broken' : ''}`} aria-hidden="true">
          {stage === 'broken' ? <ImageOffIcon /> : item.kind === 'video' ? <PlayIcon /> : item.kind === 'audio' ? <AudioIcon /> : <ImageIcon />}
        </span>}
    </span>
  );
}
