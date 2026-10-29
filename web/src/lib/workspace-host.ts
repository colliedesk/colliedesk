// localPath is path when it names a page of this site, else "/": never an address of another host. A
// browser drops tabs and line breaks from an address ("/\t/evil.com" opens //evil.com), so every control
// character is refused.
export function localPath(path: string | undefined): string {
  if (!path || !path.startsWith('/') || path.startsWith('//') || /[\\\x00-\x1f\x7f]/.test(path)) return '/';
  return path;
}
