import { lazyPart } from '../../ui/lazy-part';

// The tip of a rare state in the inbox list (a refreshed Reports number) comes from its own chunk: HelpTip brings
// Floating UI, which the first bundle does not carry.
export const helpTipPart = lazyPart(() => import('../../ui/help-tip'));
