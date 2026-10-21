import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query';
import { SearchIcon } from '../../ui/icons';
import { Icon } from './editor/icons';
import { kbMessages } from './kb-messages';
import { reason } from './kb-tree';
import { KBDialog, usePhone } from './kb-ui';
import { mediaApi, updateFields } from './media-api';
import type { KBMedia, MediaKind } from './media-api';
import { cardLabel, facts, mediaName } from './media-grid';
import { mediaMessages } from './media-messages';

// MediaPicker is the editor's way into the library: a dialog where several items are picked, in the order
// they go into the article, each with its alt text (a video's title). The alt text comes from the library;
// an image without one asks for it, and what is typed is saved to the library too, so the next article gets
// it. The grid is one Tab stop: arrows move, Space picks, Enter inserts. On a phone it fills the screen, in
// two steps: pick, then the alt text of the picks on one screen.
export function MediaPicker({ onInsert, onUpload, onClose }: { onInsert: (items: KBMedia[]) => void; onUpload: () => void; onClose: () => void }) {
  const id = useId();
  const phone = usePhone();
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  const [kind, setKind] = useState<MediaKind>();
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), 250); return () => window.clearTimeout(timer); }, [text]);
  const list = useInfiniteQuery({
    queryKey: ['kb-media', { q: q || undefined, kind, for: 'picker' }],
    queryFn: ({ pageParam, signal }) => mediaApi.list({ q: q || undefined, kind, cursor: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  });
  const items = useMemo(() => list.data?.pages.flatMap((page) => page.items) ?? [], [list.data]);
  // picked keeps the order of the clicks: it is the order the items go into the article.
  const [picked, setPicked] = useState<KBMedia[]>([]);
  const [alts, setAlts] = useState<Record<string, string>>({});
  const [step, setStep] = useState<'pick' | 'alt'>('pick');
  const [focus, setFocus] = useState(0);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const grid = useRef<HTMLUListElement>(null);
  const toggle = (item: KBMedia) => setPicked((current) => (current.some((each) => each.id === item.id) ? current.filter((each) => each.id !== item.id) : [...current, item]));
  const altOf = (item: KBMedia) => alts[item.id] ?? item.alt;
  // An image needs alt text before it goes in; a video's title is optional (its name stands in).
  const missing = picked.filter((item) => item.kind === 'image' && !altOf(item).trim());
  const ready = picked.length > 0 && missing.length === 0;
  const insert = async () => {
    if (!ready || busy) return;
    setBusy(true);
    setProblem(undefined);
    try {
      // The alt text typed here goes to the library; kept is sent as it is, since the server keeps an item whose
      // alt text a person writes unless told otherwise.
      // Each pick as the list holds it now: a poster drawn since the pick is a newer version of it.
      const saved = await Promise.all(picked.map(async (pick) => {
        const item = items.find((each) => each.id === pick.id) ?? pick;
        const alt = altOf(pick).trim();
        if (alt === item.alt) return item;
        return updateFields(item, { alt, kept: item.kept });
      }));
      onInsert(saved);
    } catch (error) {
      setProblem(reason(error, kbMessages.pickerSaveFailed));
      setBusy(false);
    }
  };
  // Roving focus: one option is the Tab stop; the arrows move it by one, or by a row.
  const columns = () => {
    const options = [...(grid.current?.querySelectorAll<HTMLElement>('[role=option]') ?? [])];
    const top = options[0]?.getBoundingClientRect().top;
    const first = options.filter((option) => option.getBoundingClientRect().top === top).length;
    return Math.max(1, first || 1);
  };
  const onGridKey = (event: KeyboardEvent<HTMLUListElement>) => {
    if (items.length === 0) return;
    // The option with the focus, read from the page: a focus that moved just now is not in the state yet.
    const options = [...(grid.current?.querySelectorAll<HTMLElement>('[role=option]') ?? [])];
    const here = Math.max(0, options.indexOf(document.activeElement as HTMLElement));
    const across = columns();
    const moves: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: across, ArrowUp: -across };
    if (event.key in moves || event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : Math.min(items.length - 1, Math.max(0, here + moves[event.key]!));
      setFocus(next);
      options[next]?.focus();
      return;
    }
    if (event.key === ' ') { event.preventDefault(); const item = items[here]; if (item) toggle(item); return; }
    if (event.key === 'Enter') {
      event.preventDefault();
      if (phone) setStep('alt');
      else if (ready) void insert();
      else document.getElementById(`${id}-alt-${missing[0]?.id}`)?.focus();
    }
  };
  const order = (item: KBMedia) => picked.findIndex((each) => each.id === item.id);
  const altFields = (compact: boolean) => picked.map((item) => {
    const video = item.kind === 'video';
    const needs = !video && !item.alt.trim();
    return (
      <div key={item.id} className="picker-pick" data-needs={needs && !altOf(item).trim() ? 'true' : undefined}>
        {compact && <span className="picker-pick-thumb">{item.thumbnail_url ? <img src={item.thumbnail_url} alt="" /> : <span className="media-picture-empty" />}</span>}
        <div className="picker-pick-text">
          <span className="picker-pick-name" title={mediaName(item)}>{mediaName(item)}</span>
          <span className="kb-hint">{video ? kbMessages.pickerVideoTitle : needs ? kbMessages.pickerNeedsAlt : kbMessages.pickerAltFromLibrary}</span>
        </div>
        <label className="field picker-pick-field"><span className="sr-only">{video ? kbMessages.pickerTitleFor(mediaName(item)) : kbMessages.pickerAltFor(mediaName(item))}</span>
          <input id={`${id}-alt-${item.id}`} value={altOf(item)} maxLength={500} placeholder={video ? mediaName(item) : kbMessages.pickerAltPlaceholder}
            aria-invalid={needs && !altOf(item).trim() ? true : undefined} onChange={(event) => setAlts((current) => ({ ...current, [item.id]: event.target.value }))} /></label>
      </div>
    );
  });
  const insertLabel = kbMessages.insertCount(picked.length);
  if (phone && step === 'alt') {
    return (
      <KBDialog title={kbMessages.pickerAltHeading(picked.length)} phone="full" className="picker picker-alts" returnFocus={false} onClose={() => setStep('pick')} closeLabel={kbMessages.pickerBack}
        footer={<>
          <button type="button" onClick={() => setStep('pick')}>{kbMessages.pickerBack}</button>
          <button type="button" className="primary-action" disabled={!ready || busy} onClick={() => void insert()}>{insertLabel}</button>
        </>}>
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {altFields(false)}
        <p className="kb-hint">{kbMessages.pickerSavedToLibrary}</p>
      </KBDialog>
    );
  }
  return (
    <KBDialog title={mediaMessages.pickerHeading} phone="full" className="picker" returnFocus={false} closeLabel={mediaMessages.close} onClose={onClose}
      footer={<>
        <span className="picker-hint" aria-hidden="true"><kbd>←</kbd><kbd>→</kbd> {kbMessages.pickerKeysMove} <kbd>Space</kbd> {kbMessages.pickerKeysSelect} <kbd>Enter</kbd> {kbMessages.pickerKeysInsert}</span>
        <span className="picker-strip" role="status">{picked.length > 0 ? kbMessages.pickerStrip(picked.length, missing.length) : kbMessages.pickerNone}</span>
        <button type="button" onClick={onClose}>{kbMessages.cancel}</button>
        {phone
          ? <button type="button" className="primary-action" disabled={picked.length === 0} onClick={() => setStep('alt')}>{kbMessages.pickerNext}</button>
          : <button type="button" className="primary-action" disabled={!ready || busy} aria-describedby={missing.length ? `${id}-missing` : undefined} onClick={() => void insert()}>{insertLabel}</button>}
      </>}>
      <div className="picker-tools">
        <label className="search-field picker-search">
          <SearchIcon />
          <span className="sr-only">{mediaMessages.search}</span>
          {/* On a phone the keyboard would cover the grid, so the search does not take the focus. */}
          <input type="search" value={text} placeholder={mediaMessages.searchPlaceholder} maxLength={200} data-autofocus={phone ? undefined : 'true'} onChange={(event) => setText(event.target.value)} />
        </label>
        <fieldset className="segmented">
          <legend className="sr-only">{mediaMessages.kind}</legend>
          {([[undefined, mediaMessages.anyKind], ['image', mediaMessages.images], ['video', mediaMessages.videos]] as const).map(([value, label]) => (
            <label key={label}><input type="radio" name={`${id}-kind`} checked={kind === value} onChange={() => { setKind(value); setFocus(0); }} />{label}</label>
          ))}
        </fieldset>
        <button type="button" onClick={onUpload}><Icon name="upload" />{kbMessages.pickerUploadNew}</button>
      </div>
      <div className="picker-body">
        <div className="picker-grid">
          {list.isError && <p role="alert" className="error-copy">{mediaMessages.listFailed}</p>}
          {list.isPending && <p role="status" className="empty-copy">{mediaMessages.loading}</p>}
          {list.isSuccess && items.length === 0 && <p role="status" className="empty-copy">{q || kind ? mediaMessages.noMatches : mediaMessages.pickerEmpty}</p>}
          {items.length > 0 && <ul ref={grid} role="listbox" aria-label={mediaMessages.heading} aria-multiselectable="true" onKeyDown={onGridKey}>
            {items.map((item, at) => {
              const place = order(item);
              const noAlt = item.kind === 'image' && !item.alt.trim();
              return (
                <li key={item.id} role="presentation">
                  <div role="option" className="picker-card" aria-selected={place >= 0} tabIndex={at === Math.min(focus, items.length - 1) ? 0 : -1} aria-label={cardLabel(item)}
                    onClick={() => { setFocus(at); toggle(item); }} onFocus={() => setFocus(at)}>
                    <span className="picker-order" aria-hidden="true">{place >= 0 ? place + 1 : ''}</span>
                    <span className="media-picture" data-kind={item.kind} data-empty={item.thumbnail_url ? undefined : 'true'}>
                      {item.thumbnail_url ? <img src={item.thumbnail_url} alt="" loading="lazy" decoding="async" /> : <span className="media-picture-empty" aria-hidden="true" />}
                      {item.kind === 'video' && item.duration_ms !== null && <span className="media-duration">{mediaMessages.duration(item.duration_ms)}</span>}
                    </span>
                    <span className="picker-card-name"><span title={mediaName(item)}>{mediaName(item)}</span>{noAlt && <span className="badge media-flag-note">{mediaMessages.noAlt}</span>}</span>
                    <span className="sr-only">{facts(item)}</span>
                  </div>
                </li>
              );
            })}
          </ul>}
          {list.hasNextPage && <button type="button" className="kb-more" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{mediaMessages.loadMore}</button>}
        </div>
        <aside className="picker-picks" aria-label={kbMessages.pickerPicked}>
          <h3>{picked.length > 0 ? kbMessages.pickerOrder(picked.length) : kbMessages.pickerNone}</h3>
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          {missing.length > 0 && <p id={`${id}-missing`} className="kb-hint">{kbMessages.pickerMissing(missing.length)}</p>}
          {altFields(true)}
        </aside>
      </div>
    </KBDialog>
  );
}
