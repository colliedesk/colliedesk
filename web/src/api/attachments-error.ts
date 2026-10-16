// AttachmentsTooLargeError is the 413 answer to a message: too many files or too many bytes. Its
// message is the server's plain reason; nothing was stored or sent.
export class AttachmentsTooLargeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AttachmentsTooLargeError';
  }
}

// AttachmentStorageError is the refusal of a message's files for lack of room: the plan's storage
// cap (403 storage_limit_reached) or a full server disk (507). Its message is the server's reason.
export class AttachmentStorageError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AttachmentStorageError';
  }
}
