// The files an article may list, as the server judges them by name before any byte is sent (it then
// checks the first bytes too). A browser often gives no type, or a wrong one, for Office files and
// archives, so the name decides here.
export const fileExtensions = ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'csv', 'txt', 'rtf', 'zip', '7z', 'tar.gz'] as const;
export const maxFileBytes = 25 << 20;
export const maxArticleFileBytes = 100 << 20;
// What the file inputs offer; .tar.gz is offered as .gz, the only part a browser matches.
export const fileAccept = fileExtensions.map((extension) => `.${extension === 'tar.gz' ? 'gz' : extension}`).join(',');

// fileExtension is the name's extension in lower case, tar.gz counted as one; '' without one.
export function fileExtension(name: string): string {
  const lower = name.toLowerCase();
  if (lower.endsWith('.tar.gz')) return 'tar.gz';
  const dot = lower.lastIndexOf('.');
  return dot > 0 ? lower.slice(dot + 1) : '';
}

export function isAttachableName(name: string): boolean {
  return (fileExtensions as readonly string[]).includes(fileExtension(name));
}

// fileFormat is the short type a person reads on a card: PDF, DOCX, TAR.GZ.
export function fileFormat(name: string): string {
  return fileExtension(name).toUpperCase();
}
