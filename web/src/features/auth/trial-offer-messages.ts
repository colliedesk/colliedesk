// Words of the page a trial offer link opens (/trial-offer?token=…).
export const trialOfferMessages = {
  loading: 'Reading the offer…',
  signInTitle: 'Sign in to continue your trial',
  signInCloud: 'Sign in to Colliedesk as an admin of the workspace that got this link; you come back here.',
  signInSelfHost: 'Open your Colliedesk sign-in link in this browser, then come back to this page.',
  signIn: 'Sign in',
  tryAgain: 'I signed in',
  title: (workspace: string) => `Continue the trial of ${workspace}`,
  offer: 'Continue your trial: 14 days, starting when you press Continue.',
  accept: 'Continue trial',
  accepting: 'Continuing…',
  accepted: (ends: string) => `Your trial now ends ${ends}.`,
  kept: (ends: string) => `Your trial already ends later, ${ends}, and stays so.`,
  open: 'Open Colliedesk',
  refusedTitle: 'This offer cannot be used',
  refusals: {
    accepted: 'This offer was already accepted.',
    expired: 'This offer has expired.',
    revoked: 'This offer was withdrawn.',
  },
  hasPlan: (workspace: string) => `${workspace} has a plan, so it needs no trial. Nothing changed.`,
  notAdmin: (names: string) => `Only an admin can accept this offer. Ask ${names} to open this link.`,
  notAdminNoOne: 'Only an admin can accept this offer, and this workspace has none who can. Write to support.',
  trialEnds: (ends: string) => `Your trial ends ${ends}.`,
  trialEnded: (ends: string) => `Your trial ended ${ends}.`,
  moreTime: 'For more time, write to',
  supportEmail: 'support@colliedesk.com',
  otherWorkspace: 'Sign in to another workspace',
  failed: 'Colliedesk could not read this offer. Try again.',
  acceptFailed: 'Colliedesk could not continue the trial. Try again.',
} as const;

// namesList joins names for a sentence: "Ada", "Ada or Grace", "Ada, Grace or Lin".
export function namesList(names: readonly string[]): string {
  return names.length <= 1 ? names.join('') : `${names.slice(0, -1).join(', ')} or ${names.at(-1)}`;
}
