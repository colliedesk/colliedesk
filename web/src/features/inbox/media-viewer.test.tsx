import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Attachment } from '../../api/schemas';
import type { GalleryItem } from './media-kind';
import { MediaGallery, MediaPlayer } from './media-viewer';

// jsdom has no media engine: play and pause are stubbed to fire the events a browser fires.
beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
    Object.defineProperty(this, 'paused', { configurable: true, value: false });
    this.dispatchEvent(new Event('play'));
    return Promise.resolve();
  });
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function (this: HTMLMediaElement) {
    Object.defineProperty(this, 'paused', { configurable: true, value: true });
    this.dispatchEvent(new Event('pause'));
  });
});
afterEach(() => vi.restoreAllMocks());

const file = (id: string, filename: string, content_type: string): Attachment => ({ id, message_id: 'm1', filename, content_type, size: 4096, sha256: 'a'.repeat(64), inline: false, created_at: '2026-11-04T09:35:00Z' });
const items: GalleryItem[] = [
  { file: file('a1', 'image-1', 'image/jpeg'), kind: 'image', sender: 'Đào Toàn', at: '2026-11-04T09:35:00Z' },
  { file: file('a2', 'clip', 'video/mp4'), kind: 'video', sender: 'Đào Toàn', at: '2026-11-04T09:50:00Z' },
  { file: file('a3', 'voice', 'audio/aac'), kind: 'audio', sender: 'Mai', at: '2026-11-04T11:14:00Z' },
];

function setDuration(media: HTMLMediaElement, seconds: number) {
  Object.defineProperty(media, 'duration', { configurable: true, value: seconds });
  fireEvent(media, new Event('loadedmetadata'));
}

test('the player plays and pauses from its own button, seeks with a slider and shows the time', () => {
  const { container } = render(<MediaPlayer kind="video" src="/api/v1/attachments/a2" title="clip" />);
  const video = container.querySelector('video')!;
  expect(video.controls).toBe(false);
  setDuration(video, 135);
  fireEvent.click(screen.getByRole('button', { name: 'Play' }));
  expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
  expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument();
  const seek = screen.getByRole('slider', { name: 'Seek' });
  fireEvent.change(seek, { target: { value: '42' } });
  expect(video.currentTime).toBe(42);
  fireEvent(video, new Event('timeupdate'));
  expect(seek).toHaveAttribute('aria-valuetext', '0:42 of 2:15');
  expect(screen.getByText('0:42 / 2:15')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Pause' }));
  expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled();
});

test('the player mutes, sets the volume and the speed, and offers full screen for a video only', () => {
  const { container, rerender } = render(<MediaPlayer kind="video" src="/v" title="clip" />);
  const video = container.querySelector('video')!;
  fireEvent.click(screen.getByRole('button', { name: 'Mute' }));
  expect(video.muted).toBe(true);
  expect(screen.getByRole('button', { name: 'Unmute' })).toBeInTheDocument();
  fireEvent.change(screen.getByRole('slider', { name: 'Volume' }), { target: { value: '0.5' } });
  expect(video.volume).toBe(0.5);
  fireEvent.click(screen.getByRole('button', { name: 'Playback speed: 1×' }));
  expect(video.playbackRate).toBe(1.25);
  expect(screen.getByRole('button', { name: 'Full screen' })).toBeInTheDocument();
  rerender(<MediaPlayer kind="audio" src="/a" title="voice" />);
  expect(screen.queryByRole('button', { name: 'Full screen' })).toBeNull();
});

test('keys on the player: k plays, m mutes; a video that cannot play says so and offers the file', () => {
  const onError = vi.fn();
  const { container } = render(<MediaPlayer kind="video" src="/v" title="clip" onError={onError} />);
  const player = container.querySelector('.media-player')!;
  fireEvent.keyDown(player, { key: 'k' });
  expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
  fireEvent.keyDown(player, { key: 'm' });
  expect(container.querySelector('video')!.muted).toBe(true);
  fireEvent.error(container.querySelector('video')!);
  expect(onError).toHaveBeenCalled();
});

test('the gallery shows where it is, who sent it and when, and moves with buttons and arrow keys', () => {
  const onClose = vi.fn();
  render(<MediaGallery items={items} start={0} timezone="UTC" onClose={onClose} />);
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText('1 / 3')).toBeInTheDocument();
  expect(within(dialog).getByRole('heading', { name: 'image-1' })).toBeInTheDocument();
  expect(within(dialog).getByText(/Đào Toàn/)).toBeInTheDocument();
  expect(within(dialog).getByRole('img', { name: 'image-1' })).toHaveAttribute('src', '/api/v1/attachments/a1');
  expect(within(dialog).getByRole('link', { name: 'Download image-1' })).toHaveAttribute('href', '/api/v1/attachments/a1');
  expect(within(dialog).getByRole('button', { name: 'Previous' })).toBeDisabled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Next' }));
  expect(within(dialog).getByText('2 / 3')).toBeInTheDocument();
  expect(dialog.querySelector('video')).toHaveAttribute('preload', 'metadata');
  expect(dialog.querySelector('video')!.controls).toBe(false);
  fireEvent.keyDown(dialog, { key: 'ArrowRight' });
  expect(within(dialog).getByText('3 / 3')).toBeInTheDocument();
  expect(within(dialog).getByText(/Mai/)).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'Next' })).toBeDisabled();
  fireEvent.keyDown(dialog, { key: 'ArrowLeft' });
  expect(within(dialog).getByText('2 / 3')).toBeInTheDocument();
  fireEvent.keyDown(dialog, { key: 'Escape' });
  expect(onClose).toHaveBeenCalled();
});

test('the strip of thumbnails jumps to an item; arrow keys on a slider seek and do not change the item', () => {
  render(<MediaGallery items={items} start={1} timezone="UTC" onClose={vi.fn()} />);
  const dialog = screen.getByRole('dialog');
  const strip = within(dialog).getByRole('list', { name: 'All media in this conversation' });
  expect(within(strip).getAllByRole('button')).toHaveLength(3);
  expect(within(strip).getByRole('button', { name: 'clip' })).toHaveAttribute('aria-current', 'true');
  fireEvent.keyDown(within(dialog).getByRole('slider', { name: 'Seek' }), { key: 'ArrowRight' });
  expect(within(dialog).getByText('2 / 3')).toBeInTheDocument();
  fireEvent.click(within(strip).getByRole('button', { name: 'voice' }));
  expect(within(dialog).getByText('3 / 3')).toBeInTheDocument();
});

test('changing the item pauses what was playing', () => {
  render(<MediaGallery items={items} start={1} timezone="UTC" onClose={vi.fn()} />);
  const dialog = screen.getByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Play' }));
  fireEvent.keyDown(dialog, { key: 'ArrowRight' });
  expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled();
});

test('the gallery starts on Close and stops a clip still playing on the page behind it', () => {
  const behind = document.createElement('audio');
  document.body.append(behind);
  void behind.play();
  render(<MediaGallery items={items} start={0} timezone="UTC" onClose={vi.fn()} />);
  expect(behind.paused).toBe(true);
  expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus();
  behind.remove();
});

test('the strip loads only the pictures near the one shown', () => {
  const many: GalleryItem[] = Array.from({ length: 12 }, (_, n) => ({ file: file(`p${n}`, `photo-${n}`, 'image/jpeg'), kind: 'image' as const, sender: 'Mai', at: '2026-11-04T09:35:00Z' }));
  render(<MediaGallery items={many} start={0} timezone="UTC" onClose={vi.fn()} />);
  const strip = screen.getByRole('list', { name: 'All media in this conversation' });
  expect(strip.querySelectorAll('img')).toHaveLength(4);
  // The strip shows thumbnails; the picture shown large is the original.
  expect(strip.querySelector('img')).toHaveAttribute('src', '/api/v1/attachments/p0/thumbnail');
  expect(screen.getByRole('img', { name: 'photo-0' })).toHaveAttribute('src', '/api/v1/attachments/p0');
});

// Safari moves Tab past buttons, so trapping Tab at the first and last control is not enough: the page behind
// is inert while the gallery is open, and comes back when it closes.
test('the page behind the gallery is inert while it is open, and focus goes back when it closes', () => {
  const page = document.createElement('div');
  const opener = document.createElement('button');
  const field = document.createElement('textarea');
  page.append(opener, field);
  document.body.append(page);
  opener.focus();
  const { unmount } = render(<MediaGallery items={items} start={0} timezone="UTC" onClose={vi.fn()} />);
  const dialog = screen.getByRole('dialog');
  expect(page).toHaveAttribute('inert');
  expect(dialog.closest('[inert]')).toBeNull();
  unmount();
  expect(page).not.toHaveAttribute('inert');
  expect(opener).toHaveFocus();
  page.remove();
});

// Safari moves Tab past buttons, and with the page inert it would leave the gallery for the window itself: the
// gallery moves Tab through its own controls, and Escape and the arrows still work if the focus was lost.
test('Tab moves through the gallery controls itself, and wraps', () => {
  render(<MediaGallery items={items} start={0} timezone="UTC" onClose={vi.fn()} />);
  const dialog = screen.getByRole('dialog');
  const close = screen.getByRole('button', { name: 'Close' });
  expect(close).toHaveFocus();
  expect(fireEvent.keyDown(close, { key: 'Tab' })).toBe(false);
  expect(screen.getByRole('button', { name: 'Next' })).toHaveFocus();
  const last = within(dialog).getByRole('button', { name: 'voice' });
  last.focus();
  expect(fireEvent.keyDown(last, { key: 'Tab' })).toBe(false);
  expect(dialog.contains(document.activeElement)).toBe(true);
  expect(within(dialog).getByRole('link', { name: 'Download image-1' })).toHaveFocus();
  fireEvent.keyDown(document.activeElement!, { key: 'Tab', shiftKey: true });
  expect(within(dialog).getByRole('button', { name: 'voice' })).toHaveFocus();
});

test('with the focus lost to the page, Escape and the arrows still reach the gallery', () => {
  const onClose = vi.fn();
  render(<MediaGallery items={items} start={0} timezone="UTC" onClose={onClose} />);
  (document.activeElement as HTMLElement | null)?.blur();
  fireEvent.keyDown(document.body, { key: 'ArrowRight' });
  expect(screen.getByText('2 / 3')).toBeInTheDocument();
  fireEvent.keyDown(document.body, { key: 'Tab' });
  expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true);
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(onClose).toHaveBeenCalled();
});

test('closing gives the focus back to what opened the gallery, even when the press did not focus it (Safari)', () => {
  const opener = document.createElement('button');
  opener.dataset.mediaOpen = 'a1';
  document.body.append(opener);
  (document.activeElement as HTMLElement | null)?.blur();
  const { unmount } = render(<MediaGallery items={items} start={0} timezone="UTC" onClose={vi.fn()} opener='[data-media-open="a1"]' />);
  unmount();
  expect(opener).toHaveFocus();
  opener.remove();
});

// A strip item not loaded yet says what it is (a picture mark), never an empty square that looks broken; a
// picture that cannot be drawn at all says so in the strip and in the frame, with the file to download.
test('strip items not loaded show a picture mark, and a picture that cannot be drawn says so', () => {
  const many: GalleryItem[] = Array.from({ length: 6 }, (_, n) => ({ file: file(`q${n}`, `pic-${n}`, 'image/jpeg'), kind: 'image' as const, sender: 'Mai', at: '2026-11-04T09:35:00Z' }));
  render(<MediaGallery items={many} start={0} timezone="UTC" onClose={vi.fn()} />);
  const strip = screen.getByRole('list', { name: 'All media in this conversation' });
  const far = within(strip).getByRole('button', { name: 'pic-5' });
  expect(far.querySelector('img')).toBeNull();
  expect(far.querySelector('.media-strip-mark svg')).not.toBeNull();
  // The thumbnail fails, then the original: a broken-picture mark, not the browser's broken image.
  const near = within(strip).getByRole('button', { name: 'pic-1' });
  fireEvent.error(near.querySelector('img')!);
  expect(near.querySelector('img')).toHaveAttribute('src', '/api/v1/attachments/q1');
  fireEvent.error(near.querySelector('img')!);
  expect(near.querySelector('img')).toBeNull();
  expect(near.querySelector('.media-strip-mark.is-broken')).not.toBeNull();
  // The large picture that cannot be drawn: words for a picture, and the download.
  fireEvent.error(screen.getByRole('img', { name: 'pic-0' }));
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText(/This picture cannot be shown here/)).toBeInTheDocument();
  expect(within(dialog).queryByText(/cannot be played/)).toBeNull();
  expect(within(dialog).getAllByRole('link', { name: 'Download pic-0' }).length).toBeGreaterThan(0);
});
