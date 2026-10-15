import type { ReactNode } from 'react';

const urlPattern = /https?:\/\/[^\s<>"')\]]+/g;
// Mail clients hard-wrap text/plain near 72-78 characters (Gmail does). A line that long followed by
// more text is a wrap, not a break the customer typed; lists, quotes and signatures always keep theirs.
const wrapWidth = 60;
const keepsItsBreak = /^\s*(?:[-*+•]\s|\d+[.)]\s|>|--\s*$)/;

// unwrapHardWraps joins wrapped lines back into paragraphs for display only; the stored body is unchanged.
export function unwrapHardWraps(text: string): string {
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  const out: string[] = [];
  for (const line of lines) {
    const previous = out.at(-1);
    const joins = previous !== undefined && previous.trimEnd().length >= wrapWidth && !previous.startsWith('>')
      && line.trim() !== '' && !keepsItsBreak.test(line);
    if (joins) out[out.length - 1] = `${previous.trimEnd()} ${line.trimStart()}`;
    else out.push(line);
  }
  return out.join('\n');
}

// Customer email is untrusted: show it as written. No Markdown, so no remote images and no link
// whose text differs from its target; visible http(s) URLs become links that show their real href.
export function PlainTextBody({ text: raw }: { text: string }) {
  const text = unwrapHardWraps(raw);
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
