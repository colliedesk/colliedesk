// The strings of the console's cloud sign-in, a lazy part: they load with it, not in the console's first bundle.
// Another language's words wait in lazyWords (lang.ts) and replace these as it loads.
import { lazyWords } from './lang';

const en = {
  signingOut: 'Signing out…',
  signOutFailed: 'Signing out did not work. Try again.',
  retry: 'Try signing in again',
  refused: 'This account cannot sign in to the operator console.',
  refusedHelp: 'If you run this server, an operator adds you on it with:',
  useAnotherAccount: 'Use another account',
};

export const cloudMessages: typeof en = Object.assign(en, lazyWords.cloud);
