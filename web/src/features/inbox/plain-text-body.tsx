import type { ReactNode } from 'react';

const urlPattern = /https?:\/\/[^\s<>"')\]]+/g;

// Customer email is untrusted: show it as written. No Markdown, so no remote images and no link
// whose text differs from its target; visible http(s) URLs become links that show their real href.
export function PlainTextBody({ text }: { text: string }) {
  const parts: ReactNode[] = [];
  let last = 0;
  for (const match of text.matchAll(urlPattern)) {
    const url = match[0].replace(/[.,;:!?]+$/, '');
    const start = match.index ?? 0;
    parts.push(text.slice(last, start));
    parts.push(<a key={start} href={url} target="_blank" rel="noopener noreferrer nofollow">{url}</a>);
    last = start + url.length;
  }
  parts.push(text.slice(last));
  return <p className="plain-text-body">{parts}</p>;
}
