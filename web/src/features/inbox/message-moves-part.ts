import { lazyPart } from '../../ui/lazy-part';

// Moving messages (the menu on each customer message, the move dialog and the moves log) is a chunk of its
// own, loaded once a conversation is open; the first bundle carries none of it.
export const messageMovesPart = lazyPart(() => import('./message-moves'));
