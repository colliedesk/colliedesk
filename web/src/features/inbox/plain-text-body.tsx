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

// tidyPlainText removes what only adds height: stored plain bodies made from HTML mail hold runs of
// lines that are nothing but spaces (up to 105 of them), which pre-wrap would draw as tall gaps.
// Runs of blank lines become one, trailing spaces go, leading and trailing blank lines go. The
// leading spaces of a text line stay: signatures and indented lists use them.
export function tidyPlainText(text: string): string {
  const out: string[] = [];
  for (const raw of text.replace(/\r\n/g, '\n').split('\n')) {
    const line = raw.trimEnd();
    if (line === '' && (out.length === 0 || out.at(-1) === '')) continue;
    out.push(line);
  }
  while (out.at(-1) === '') out.pop();
  return out.join('\n');
}

// Customer email is untrusted: show it as written. No Markdown, so no remote images and no link
// whose text differs from its target; visible http(s) URLs become links that show their real href.
// keepBreaks is for chat, where every line break is one the customer typed, never a mail client's wrap.
export function PlainTextBody({ text: raw, keepBreaks = false }: { text: string; keepBreaks?: boolean }) {
  const tidy = tidyPlainText(raw);
  const text = keepBreaks ? tidy : unwrapHardWraps(tidy);
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
