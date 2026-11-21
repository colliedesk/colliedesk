// articleAddress is the part of an article's public address that stays the same on every host of the help center:
// /a/<number>-<slug>. The help center puts its own path in front when it shows the article, so a link written this
// way keeps working when the help center moves.
export function articleAddress(url: string): string {
  const { pathname } = new URL(url);
  const at = pathname.lastIndexOf('/a/');
  return at < 0 ? pathname : pathname.slice(at);
}
