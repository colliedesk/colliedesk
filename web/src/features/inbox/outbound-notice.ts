import type { EmailChannelSettings } from '../../api/schemas';
import { messages } from '../../messages';

// outboundNotice is what the composer says when replies cannot be emailed now, or undefined when they can.
// A broken Gmail connection is named as the cause: replies then never leave through SMTP. An admin is told
// to reconnect; anyone else to ask an admin.
export function outboundNotice(settings: EmailChannelSettings | undefined, canManage = false): string | undefined {
  if (!settings?.channel || settings.sending_from !== null) return undefined;
  if (settings.gmail?.connection?.status === 'failed') return messages.outboundGmailFailed(canManage);
  if (settings.gmail?.connection && !settings.gmail.available) return messages.outboundGmailNotSetUp;
  return settings.smtp.configured ? messages.outboundNeedsVerification : messages.outboundNotConfigured;
}
