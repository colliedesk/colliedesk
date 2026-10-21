import { memo } from 'react';
import type { MouseEvent } from 'react';
import { formatBytes } from '../../lib/format-bytes';
import type { KBMedia } from './media-api';
import { mediaMessages } from './media-messages';
import { Icon } from './editor/icons';

// The cards of the media library: a 4:3 picture (the thumbnail, a video's poster thumbnail), a video's
// length on it, the name and the size. A card opens the item, or, in a page that selects, ticks it.

export function mediaName(item: KBMedia) {
  return item.name || mediaMessages.unnamed;
}

export function usageLabel(item: KBMedia) {
  const usage = item.usage;
  if (!usage) return undefined;
  if (usage.articles > 0) return mediaMessages.usedIn(usage.articles);
  if (usage.trashed_articles > 0) return mediaMessages.inTrashedArticles(usage.trashed_articles);
  if (usage.older_versions > 0) return mediaMessages.inOlderVersions(usage.older_versions);
  return mediaMessages.notUsed;
}

// cardLabel is what a screen reader hears of a card: its name, type, length, size and use.
export function cardLabel(item: KBMedia, extra?: string) {
  return [mediaName(item), mediaMessages.kindName(item.kind), item.kind === 'video' && item.duration_ms !== null ? mediaMessages.duration(item.duration_ms) : '',
    formatBytes(item.size), usageLabel(item) ?? '', item.kept && item.usage?.articles === 0 && extra === undefined ? mediaMessages.kept : '', extra ?? '']
    .filter(Boolean).join(', ');
}

function Picture({ item }: { item: KBMedia }) {
  return (
    <span className="media-picture" data-kind={item.kind} data-empty={item.thumbnail_url ? undefined : 'true'}>
      {item.thumbnail_url ? <img src={item.thumbnail_url} alt="" loading="lazy" decoding="async" /> : <span className="media-picture-empty" aria-hidden="true" />}
      {item.kind === 'video' && <span className="media-play" aria-hidden="true"><svg viewBox="0 0 24 24" width="12" height="12"><path d="M8 5v14l11-7Z" fill="currentColor" /></svg></span>}
      {item.kind === 'video' && item.duration_ms !== null && <span className="media-duration">{mediaMessages.duration(item.duration_ms)}</span>}
    </span>
  );
}

// facts is the card's second line: the size and, for an image its dimensions, for a video its format.
export function facts(item: KBMedia) {
  const format = item.kind === 'video' ? item.content_type.replace('video/', '').toUpperCase().replace('WEBM', 'WebM')
    : item.width !== null && item.height !== null ? `${item.width}×${item.height}` : '';
  return [formatBytes(item.size), format].filter(Boolean).join(' · ');
}

// A card shows only what is out of the ordinary: no alt text, not used, kept (a pin). The rest is in the panel.
function Caption({ item, reason }: { item: KBMedia; reason?: string }) {
  const unused = item.usage !== undefined && item.usage.articles === 0;
  const noAlt = item.kind === 'image' && !item.alt.trim();
  return (
    <span className="media-caption">
      <span className="media-name" title={mediaName(item)}>{mediaName(item)}</span>
      <span className="media-meta">
        <span className="media-facts-line">{facts(item)}</span>
        {/* One flag at most, so the size stays readable: not used first, then a used image without alt text. */}
        {reason === undefined && (noAlt || unused) && <span className="media-flags">
          {unused ? <span className="badge" title={noAlt ? mediaMessages.notUsedNoAlt : undefined}>{mediaMessages.notUsed}{noAlt && <span aria-hidden="true"> · {mediaMessages.noAlt}</span>}</span>
            : <span className="badge media-flag-note">{mediaMessages.noAlt}</span>}
        </span>}
      </span>
      {reason && <span className="media-reason" title={reason}>{reason}</span>}
    </span>
  );
}

function Pin({ item }: { item: KBMedia }) {
  return item.kept ? <span className="media-pin" title={mediaMessages.kept} aria-hidden="true"><Icon name="pin" size={12} /></span> : null;
}

// MediaCard opens an item; aria-current marks the one open in the panel. Cards are memoized: a page may
// hold thousands, and opening one or ticking one must not draw them all again (onOpen must be stable).
export const MediaCard = memo(function MediaCard({ item, open, onOpen }: { item: KBMedia; open: boolean; onOpen: (item: KBMedia) => void }) {
  return (
    <li>
      <button type="button" className="media-card" data-media-id={item.id} aria-current={open ? 'true' : undefined} onClick={() => onOpen(item)}
        aria-label={cardLabel(item)}>
        <Pin item={item} />
        <Picture item={item} />
        <Caption item={item} />
      </button>
    </li>
  );
});

// SelectCard ticks an item; Shift with a click ticks every card between it and the last one ticked. reason says
// why the item is on the page (only in a deleted article, kept and never used…).
export const SelectCard = memo(function SelectCard({ item, checked, extra, onToggle }: { item: KBMedia; checked: boolean; extra?: string; onToggle: (item: KBMedia, range: boolean) => void }) {
  const click = (event: MouseEvent<HTMLInputElement>) => onToggle(item, event.shiftKey);
  return (
    <li>
      <label className="media-card" data-checked={checked ? 'true' : undefined}>
        <span className="media-check-box"><input type="checkbox" className="media-check" checked={checked} onChange={() => undefined} onClick={click} aria-label={mediaMessages.select(mediaName(item))} aria-describedby={`media-card-${item.id}`} /></span>
        <Pin item={item} />
        <Picture item={item} />
        <Caption item={item} reason={extra ?? usageLabel(item) ?? ''} />
        <span id={`media-card-${item.id}`} className="sr-only">{cardLabel(item, extra)}</span>
      </label>
    </li>
  );
});

// embedFor is what an article holds to show the item: Markdown for an image, the allowed <video> HTML
// for a video, as a block of its own.
export function embedFor(item: KBMedia) {
  if (item.kind === 'image') return `![${item.alt.replace(/[[\]]/g, '')}](${item.url})`;
  // A video's title for screen readers: its alt text, or its name.
  const text = item.alt || item.name;
  const title = text ? ` title="${text.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"` : '';
  return `<video controls preload="metadata" src="${item.url}"${title}></video>`;
}
