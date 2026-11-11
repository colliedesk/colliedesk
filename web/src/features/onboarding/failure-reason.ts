// failureReason is why a read of a setup step failed, in words: the server's own message when it gave one,
// the status it answered with, or that it did not answer (the network failed).
export function failureReason(error: unknown): string {
  if (error instanceof TypeError || !(error instanceof Error)) return 'the server did not answer.';
  const status = /^API (\d{3})$/.exec(error.message);
  return status ? `the server answered with an error (HTTP ${status[1]}).` : error.message;
}
