export type OutboundRefusal = 'trial_outbound_restricted' | 'trial_outbound_limit';

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

  constructor(code: OutboundRefusal, message: string, retryAfterSeconds?: number, refusedCc: string[] = [], replyTo?: string) {
    super(message);
    this.name = 'OutboundRefusedError';
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
    this.refusedCc = refusedCc;
    this.replyTo = replyTo;
  }
}
