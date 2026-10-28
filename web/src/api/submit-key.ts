// A submit key names one create request (its Idempotency-Key header): a retry of the same input sends the
// same key, so the server creates it once however many times it is asked, and returns the first answer.
// randomUUID needs a secure page; a server reached over plain HTTP gets random hex instead.
export function newSubmitKey(): string {
  return globalThis.crypto.randomUUID?.() ?? Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

