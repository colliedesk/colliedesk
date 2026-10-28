import type { Generated } from './schemas';

export type OutboundRefusal = 'trial_outbound_restricted' | 'trial_outbound_limit';
// Why the trial refused a customer who did write (the server's own words then say it all).
export type OutboundRefusalReason = NonNullable<Generated<'Error'>['error']['reason']>;

// OutboundRefusedError means the trial refused to email a reply (HTTP 403 or 429). The code says
// which rule applied; retryAfterSeconds is set for the daily cap, refusedCc when copied addresses
// were the reason.
export class OutboundRefusedError extends Error {
  readonly code: OutboundRefusal;
  readonly retryAfterSeconds?: number;
  readonly refusedCc: string[];
  // Set when the customer wrote first but from another address, merged into this contact: the address
  // the reply would go to, which never wrote.
  readonly replyTo?: string;
  readonly reason?: OutboundRefusalReason;

  constructor(code: OutboundRefusal, message: string, retryAfterSeconds?: number, refusedCc: string[] = [], replyTo?: string, reason?: OutboundRefusalReason) {
    super(message);
    this.name = 'OutboundRefusedError';
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
    this.refusedCc = refusedCc;
    this.replyTo = replyTo;
    this.reason = reason;
  }
}
