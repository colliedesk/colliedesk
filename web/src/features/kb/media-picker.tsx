import { useEffect, useRef, useState } from 'react';
import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query';
import { SearchIcon } from '../../ui/icons';
import { mediaApi } from './media-api';
import type { KBMedia, MediaKind } from './media-api';
import { MediaCard } from './media-grid';
import { mediaMessages } from './media-messages';

// MediaPicker is the editor's way into the library: the same cards, a search and the type, newest first.
// Choosing one hands it to the editor, which puts it after the block the cursor is in.
export function MediaPicker({ onPick, onClose }: { onPick: (item: KBMedia) => void; onClose: () => void }) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  const [kind, setKind] = useState<MediaKind>();
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), 250); return () => window.clearTimeout(timer); }, [text]);
  const search = useRef<HTMLInputElement>(null);
  useEffect(() => { search.current?.focus(); }, []);
  const list = useInfiniteQuery({
    queryKey: ['kb-media', { q: q || undefined, kind, for: 'picker' }],
    queryFn: ({ pageParam, signal }) => mediaApi.list({ q: q || undefined, kind, cursor: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  });
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <div className="media-picker" role="group" aria-labelledby="media-picker-heading"
      onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); onClose(); } }}>
      <div className="media-picker-head">
        <h3 id="media-picker-heading">{mediaMessages.pickerHeading}</h3>
        <button type="button" onClick={onClose}>{mediaMessages.close}</button>
      </div>
      <div className="kb-filters">
        <label className="search-field kb-search">
          <SearchIcon />
          <span className="sr-only">{mediaMessages.search}</span>
          <input ref={search} type="search" value={text} placeholder={mediaMessages.search} maxLength={200} onChange={(event) => setText(event.target.value)} />
        </label>
        <fieldset className="segmented">
          <legend className="sr-only">{mediaMessages.kind}</legend>
          {([[undefined, mediaMessages.anyKind], ['image', mediaMessages.images], ['video', mediaMessages.videos]] as const).map(([value, label]) => (
            <label key={label}><input type="radio" name="media-picker-kind" checked={kind === value} onChange={() => setKind(value)} />{label}</label>
          ))}
        </fieldset>
      </div>
      {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}</p>}
      {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
      {list.isSuccess && items.length === 0 && <p role="status" className="empty-copy">{q || kind ? mediaMessages.noMatches : mediaMessages.pickerEmpty}</p>}
      {items.length > 0 && <ul className="media-grid media-picker-grid" aria-label={mediaMessages.heading}>
        {items.map((item) => <MediaCard key={item.id} item={item} open={false} onOpen={onPick} />)}
      </ul>}
      {list.hasNextPage && <button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{mediaMessages.loadMore}</button>}
    </div>
  );
}
