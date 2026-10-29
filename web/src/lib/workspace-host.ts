// localPath is path when it names a page of this site, else "/": never an address of another host.
export function localPath(path: string | undefined): string {
  if (!path || !path.startsWith('/') || path.startsWith('//') || /[\\\r\n]/.test(path)) return '/';
  return path;
}
