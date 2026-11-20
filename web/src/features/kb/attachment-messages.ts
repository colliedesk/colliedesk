import { formatBytes } from '../../lib/format-bytes';
import { fileExtensions, maxArticleFileBytes, maxFileBytes } from './kb-file-types';

// Strings of the Attachments strip under an article's or translation's text, in the knowledge base chunk.
export const attachmentMessages = {
  heading: 'Attachments',
  hint: 'Listed under the text for readers to download. Drop, paste or choose files: up to 25 MiB each, 100 MiB in all.',
  choose: 'Choose files',
  dropHere: 'Drop to attach',
  saveFirst: 'Save the article to attach files.',
  saveTranslationFirst: 'Save the translation to attach files.',
  // A translation that lists none shows the article's files on the help center.
  articleFiles: 'With no files of its own, this translation lists the article\'s files.',
  remove: (name: string) => `Remove ${name}`,
  removeTip: 'Remove from this list. The media library keeps the file.',
  removed: (name: string) => `${name} removed. It stays in the media library.`,
  uploading: (name: string, percent: number) => `Uploading ${name}… ${percent}%`,
  type: (name: string) => `${name} is not a type an article can list. Files: ${fileExtensions.map((extension) => extension.toUpperCase()).join(', ')}.`,
  tooLarge: (name: string, size: number) => `${name} is ${formatBytes(size)}, over the ${formatBytes(maxFileBytes)} limit for one file.`,
  tooMuch: (name: string, total: number) => `${name} would bring the files of this article to ${formatBytes(total)}, over the ${formatBytes(maxArticleFileBytes)} limit.`,
  failed: 'The file could not be attached. Try again.',
};
