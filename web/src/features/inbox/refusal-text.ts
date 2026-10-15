import { OutboundRefusedError } from '../../api/outbound-error';
import { formatWait } from '../../lib/format-wait';
import { messages } from '../../messages';

const defaultWaitSeconds = 3600;

// replyRefusalText says why a new reply was not sent. The composer keeps the text, so it says so.
export function replyRefusalText(error: unknown): string {
  if (error instanceof OutboundRefusedError) {
    return error.code === 'trial_outbound_limit' ? messages.trialReplyLimit(formatWait(error.retryAfterSeconds ?? defaultWaitSeconds)) : messages.trialReplyRestricted;
  }
  return messages.replyNotSent;
}

// retryRefusalText says why retrying a failed delivery was refused; there is no draft to keep.
export function retryRefusalText(error: unknown): string {
  if (error instanceof OutboundRefusedError) {
    return error.code === 'trial_outbound_limit' ? messages.trialRetryLimit(formatWait(error.retryAfterSeconds ?? defaultWaitSeconds)) : messages.trialRetryRestricted;
  }
  return messages.retryNotSent;
}
