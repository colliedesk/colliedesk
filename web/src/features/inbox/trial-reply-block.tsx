import type { Conversation, EmailChannelSettings, Message } from '../../api/schemas';
import { messages } from '../../messages';
import { contactMessages } from '../contacts/contact-messages';

// trialDomainUnverified: the workspace is on the trial and its email channel has no verified sending domain,
// the two facts that keep the trial from emailing anyone who did not write by email. Nothing is claimed
// while the plan or the channel is unknown.
export function trialDomainUnverified(plan: string | undefined, settings: EmailChannelSettings | undefined): boolean {
  return plan === 'trial' && Boolean(settings?.channel) && !settings?.channel?.domain_verified_at;
}

// typedAddressOnly: the customer of a help chat or web form conversation gave their address by typing it,
// and no mail from them has arrived through the platform since. The server stays the judge of a reply; this
// only says so before the person writes one. Unknown until the timeline has loaded.
export function typedAddressOnly(conversation: Pick<Conversation, 'channel'> | undefined, timeline: Message[] | undefined): boolean {
  if (!timeline || (conversation?.channel !== 'help_chat' && conversation?.channel !== 'web_form')) return false;
  return !timeline.some((item) => item.kind === 'incoming' && item.email?.via === 'platform' && item.email.authentication !== 'fail');
}

// TrialLinks are the two ways out of the trial's limit: verify the sending domain, or choose a plan.
export function TrialLinks({ verifyHref }: { verifyHref: string }) {
  return <><a href={verifyHref}>{contactMessages.verifySendingDomain}</a>{' · '}<a href="/settings/billing">{messages.choosePlan}</a></>;
}
