import type { EmailChannelSettings } from '../../api/schemas';
import { messages } from '../../messages';

// "a, b and c": the product's English writes lists without a comma before and.
// mailboxesToMove reads the list defensively: the inbox must render from a partial settings object too.
const mailboxesToMove = (settings: EmailChannelSettings | undefined) => settings?.mail_switch?.mailboxes ?? [];

const addressList = new Intl.ListFormat('en-GB', { style: 'long', type: 'conjunction' });

// MailSwitchLine sits under the inbox title while a mailbox of the team brought no customer mail here yet
// (a test message does not count). The server lists those mailboxes (mail_switch), leaving out a hosted
// forwarding address, which has no mailbox to move. For a mailbox the team had before its import
// (old_help_desk) that mail still reaches the old help desk; one added later is only not here yet. Its action
// opens step 2 of the first one's email setup (forwarding) for a person who can change channels.
export function MailSwitchLine({ settings, canManageChannels }: { settings?: EmailChannelSettings; canManageChannels?: boolean }) {
  const mailboxes = mailboxesToMove(settings);
  const first = mailboxes[0];
  if (!settings || !first) return null;
  const addresses = (old: boolean) => mailboxes.filter((mailbox) => mailbox.old_help_desk === old).map((mailbox) => mailbox.support_address);
  const inOldHelpDesk = addresses(true);
  const notHereYet = addresses(false);
  const said = [
    ...(inOldHelpDesk.length ? [messages.mailStillInOldHelpDesk(addressList.format(inOldHelpDesk))] : []),
    ...(inOldHelpDesk.length && settings.answers_before_forwarding === 'colliedesk' ? [messages.repliesComeBackHere] : []),
    ...(notHereYet.length ? [messages.mailNotHereYet(addressList.format(notHereYet))] : []),
  ].join(' ');
  return <p className="mail-switch">
    {said}{' '}
    {canManageChannels
      ? <a className="link-action" href={`/settings/channels/${encodeURIComponent(first.channel_id)}/setup?step=forward`}>{first.old_help_desk ? messages.moveYourMail : messages.forwardYourMailbox}</a>
      : messages.adminCanMoveMail}
  </p>;
}

// answerGoesToMailbox: on a server that cannot receive replies on its sending domain, the customer's answer to
// a reply goes to the support mailbox of this channel, which does not forward here yet.
export const answerGoesToMailbox = (settings: EmailChannelSettings | undefined) =>
  settings?.answers_before_forwarding === 'support_mailbox' && Boolean(settings.channel)
  && mailboxesToMove(settings).some((mailbox) => mailbox.channel_id === settings.channel?.channel_id);
