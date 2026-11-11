import { lazyPart } from '../../ui/lazy-part';

// The video and audio player and the media gallery are a chunk of their own: a conversation's pictures show
// from the first bundle, and the player loads when a conversation holds a video or a voice clip, or a picture
// is opened.
export const mediaViewerPart = lazyPart(() => import('./media-viewer'));
