import type { EmailChannelSettings } from '../../api/schemas';
import { addressSaved } from '../../channels/email/setup-state';
import { messages } from '../../messages';

// beforeMailSwitch: the team's own support address has brought no customer mail through its mailbox yet
// (a test message does not count), so new mail may still reach the old help desk. A channel still on its
// forwarding address has no mailbox of the team's own to move.
const beforeMailSwitch = (settings: EmailChannelSettings | undefined): settings is EmailChannelSettings =>
  Boolean(settings?.channel) && addressSaved(settings) && settings?.mail_through_mailbox === false;

// MailSwitchLine sits under the inbox title until the default mailbox forwards here. Its action opens
// step 2 of that channel's email setup (forwarding) for a person who can change the channel.
export function MailSwitchLine({ settings, canManage }: { settings?: EmailChannelSettings; canManage?: boolean }) {
  if (!beforeMailSwitch(settings)) return null;
  const id = settings.channel?.channel_id;
  return <p className="mail-switch">
    {messages.mailStillInOldHelpDesk}{settings.answers_before_forwarding === 'colliedesk' && ` ${messages.repliesComeBackHere}`}
    {canManage && id && <> <a className="link-action" href={`/settings/channels/${encodeURIComponent(id)}/setup?step=forward`}>{messages.moveYourMail}</a></>}
  </p>;
}

// answerGoesToMailbox: on a server that cannot receive replies on its sending domain, the customer's answer to
// a reply goes to the support mailbox, which does not forward here yet.
export const answerGoesToMailbox = (settings: EmailChannelSettings | undefined) =>
  beforeMailSwitch(settings) && settings.answers_before_forwarding === 'support_mailbox';
