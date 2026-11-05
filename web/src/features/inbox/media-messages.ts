// Strings of the media player and gallery (docs/user/inbox.md "Pictures, videos and voice clips"); they load
// with the media-viewer part, not with the inbox.

export const mediaMessages = {
  play: 'Play',
  pause: 'Pause',
  seek: 'Seek',
  seekText: (at: string, total: string) => `${at} of ${total}`,
  mute: 'Mute',
  unmute: 'Unmute',
  volume: 'Volume',
  speed: (rate: number) => `Playback speed: ${rate}×`,
  fullScreen: 'Full screen',
  exitFullScreen: 'Exit full screen',
  openInGallery: (name: string) => `Open ${name} in the gallery`,
  download: (name: string) => `Download ${name}`,
  notPlayable: 'This file cannot be played here. Download it to open it.',
  notShown: 'This picture cannot be shown here. Download it to open it.',
  loading: 'Loading…',
  gallery: (position: string, name: string) => `Media ${position}: ${name}`,
  position: (index: number, total: number) => `${index} / ${total}`,
  previous: 'Previous',
  next: 'Next',
  close: 'Close',
  strip: 'All media in this conversation',
};
