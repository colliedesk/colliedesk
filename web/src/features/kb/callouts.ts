import { kbMessages } from './kb-messages';

export type CalloutType = keyof typeof kbMessages.calloutLabels;
export const calloutTypes = Object.keys(kbMessages.calloutLabels) as CalloutType[];

interface Node {
  type: string;
  value?: string;
  children?: Node[];
  data?: { hName?: string; hProperties?: Record<string, unknown> };
}

const marker = /^\s*\[!([a-z]+)\]\s*$/i;

// callouts turns GitHub's alert syntax into the help center's callouts (server/internal/helpcenter/
// callout.go): a quote at the top of the article whose first line is only [!NOTE], [!TIP],
// [!IMPORTANT], [!WARNING] or [!CAUTION], in any case, becomes an aside announced as a note, with
// the type named first. Any other quote stays a quote.
export function callouts() {
  return (tree: Node) => {
    for (const quote of tree.children ?? []) {
      if (quote.type !== 'blockquote') continue;
      const paragraph = quote.children?.[0];
      if (paragraph?.type !== 'paragraph') continue;
      const type = takeMarker(paragraph);
      if (!type) continue;
      if ((paragraph.children ?? []).length === 0) quote.children!.shift();
      quote.data = { hName: 'aside', hProperties: { className: ['callout', `callout-${type}`], role: 'note' } };
      quote.children!.unshift({
        type: 'paragraph',
        data: { hProperties: { className: ['callout-label'] } },
        children: [{ type: 'text', value: kbMessages.calloutLabels[type] }],
      });
    }
  };
}

// takeMarker removes the paragraph's first line when it is a known marker alone, and names its type.
// The line ends at a newline or at a hard break (two trailing spaces or a backslash), as on the server.
export function takeMarker(paragraph: Node): CalloutType | null {
  const children = paragraph.children ?? [];
  let line = '';
  let count = 0;
  let rest = '';
  for (const child of children) {
    if (child.type === 'break') {
      count++;
      break;
    }
    if (child.type !== 'text') return null;
    const value = child.value ?? '';
    const end = value.indexOf('\n');
    count++;
    if (end >= 0) {
      line += value.slice(0, end);
      rest = value.slice(end + 1);
      break;
    }
    line += value;
  }
  const type = marker.exec(line)?.[1]?.toLowerCase() as CalloutType | undefined;
  if (!type || !calloutTypes.includes(type)) return null;
  const after = children.slice(count);
  paragraph.children = rest ? [{ type: 'text', value: rest }, ...after] : after;
  return type;
}
