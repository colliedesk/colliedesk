import { AttachmentsTooLargeError, AttachmentStorageError } from '../../api/attachments-error';
import { InvalidRequestError } from '../../api/client';
import { OutboundRefusedError } from '../../api/outbound-error';
import { formatWait } from '../../lib/format-wait';
import { messages } from '../../messages';

const defaultWaitSeconds = 3600;

// replyRefusalText says why a new reply was not sent. The composer keeps the text, so it says so.
export function replyRefusalText(error: unknown): string {
  if (error instanceof OutboundRefusedError) {
    if (error.code === 'trial_outbound_limit') return messages.trialReplyLimit(formatWait(error.retryAfterSeconds ?? defaultWaitSeconds));
    return error.refusedCc.length > 0 ? messages.trialCcRestricted(error.refusedCc) : messages.trialReplyRestricted;
  }
  if (error instanceof AttachmentsTooLargeError || error instanceof AttachmentStorageError) return error.message;
  return messages.replyNotSent;
}

// retryRefusalText says why retrying a failed delivery was refused; there is no draft to keep.
export function retryRefusalText(error: unknown): string {
  if (error instanceof OutboundRefusedError) {
    return error.code === 'trial_outbound_limit' ? messages.trialRetryLimit(formatWait(error.retryAfterSeconds ?? defaultWaitSeconds)) : messages.trialRetryRestricted;
  }
  // A refusal with a reason (no SMTP relay, Gmail broken or not set up) will not pass by trying again.
  if (error instanceof InvalidRequestError) return error.message;
  return messages.retryNotSent;
}
