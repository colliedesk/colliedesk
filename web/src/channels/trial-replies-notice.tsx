import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import { trialDomainUnverified, TrialLinks } from '../features/inbox/trial-reply-block';
import { messages } from '../messages';

// TrialRepliesNotice is the sentence on a channel whose customers type their address (help chat, web form):
// on the trial with no verified sending domain, a reply to them is refused, so it says so before any arrives.
// The queries are the inbox's own, so they cost nothing when the inbox was open first.
export function TrialRepliesNotice() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const email = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  if (!trialDomainUnverified(me.data?.plan?.plan, email.data)) return null;
  return <div className="settings-body"><p className="notice notice-plain" role="status"><span>{messages.trialTypedAddressChannels}{' '}
    <TrialLinks verifyHref="/settings/channels/type/email?tab=replies" /></span></p></div>;
}
