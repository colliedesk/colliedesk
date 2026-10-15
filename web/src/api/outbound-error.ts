export type OutboundRefusal = 'trial_outbound_restricted' | 'trial_outbound_limit';

// OutboundRefusedError means the trial refused to email a reply (HTTP 403 or 429). The code says
// which rule applied; retryAfterSeconds is set for the daily cap.
export class OutboundRefusedError extends Error {
  readonly code: OutboundRefusal;
  readonly retryAfterSeconds?: number;

  constructor(code: OutboundRefusal, message: string, retryAfterSeconds?: number) {
    super(message);
    this.name = 'OutboundRefusedError';
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}
