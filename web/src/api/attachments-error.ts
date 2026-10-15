// AttachmentsTooLargeError is the 413 answer to a message: too many files or too many bytes. Its
// message is the server's plain reason; nothing was stored or sent.
export class AttachmentsTooLargeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AttachmentsTooLargeError';
  }
}
