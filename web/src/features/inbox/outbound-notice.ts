import type { EmailChannelSettings } from '../../api/schemas';
import { messages } from '../../messages';

// outboundNotice is what the composer says when replies cannot be emailed now, or undefined when they can.
// A broken mailbox connection (Gmail, Microsoft 365) is named as the cause: replies then never leave through
// SMTP. An admin is told to reconnect; anyone else to ask an admin.
export function outboundNotice(settings: EmailChannelSettings | undefined, canManage = false): string | undefined {
  if (!settings?.channel || settings.sending_from !== null) return undefined;
  const mailbox = settings.gmail?.connection ? { settings: settings.gmail, name: 'Gmail' }
    : settings.microsoft?.connection ? { settings: settings.microsoft, name: 'Microsoft 365' } : undefined;
  if (mailbox?.settings.connection?.status === 'failed') return messages.outboundMailboxFailed(canManage, mailbox.name);
  if (mailbox && !mailbox.settings.available) return messages.outboundMailboxNotSetUp(mailbox.name);
  return settings.smtp.configured ? messages.outboundNeedsVerification : messages.outboundNotConfigured;
}

// largeAttachmentWarning is the size from which the composer warns, without stopping the reply, that it goes
// through Microsoft 365, which may refuse that many file bytes in one message. Microsoft documents no limit;
// a refused reply fails with the reason and keeps its files.
export function largeAttachmentWarning(settings: EmailChannelSettings | undefined): { bytes: number; text: string } | undefined {
  const microsoft = settings?.microsoft;
  if (!settings?.channel || microsoft?.connection?.status !== 'connected') return undefined;
  return { bytes: microsoft.large_attachment_bytes, text: messages.microsoftLargeAttachments };
}
