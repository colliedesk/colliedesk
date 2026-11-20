// lineDiff compares two Markdown texts line by line, as Compare with published shows them.

export interface DiffLine {
  change: 'same' | 'removed' | 'added';
  text: string;
}

// An empty text has no lines; \r\n and \r end a line as \n does, as the editor writes them.
const linesOf = (text: string) => (text === '' ? [] : text.replace(/\r\n?/g, '\n').split('\n'));

// Beyond this many line pairs left after the common start and end, the changed part shows as removed then
// added instead of matched line by line: the table would take hundreds of megabytes for two long, very
// different texts.
const maxCells = 4_000_000;

// lineDiff lists the lines of both texts in order: kept, removed from before, added in after. In a changed
// run the removed lines come first, then the added ones.
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = linesOf(before);
  const b = linesOf(after);
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start += 1;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) { endA -= 1; endB -= 1; }
  const n = endA - start;
  const m = endB - start;
  const out: DiffLine[] = a.slice(0, start).map((text) => ({ change: 'same', text }));
  if (n * m > maxCells) {
    for (const text of a.slice(start, endA)) out.push({ change: 'removed', text });
    for (const text of b.slice(start, endB)) out.push({ change: 'added', text });
  } else {
    // longest[i * width + j] is the longest run of lines in order that a[start+i..] and b[start+j..] share.
    const width = m + 1;
    const longest = new Uint32Array((n + 1) * width);
    for (let i = n - 1; i >= 0; i -= 1) {
      for (let j = m - 1; j >= 0; j -= 1) {
        longest[i * width + j] = a[start + i] === b[start + j] ? longest[(i + 1) * width + j + 1]! + 1
          : Math.max(longest[(i + 1) * width + j]!, longest[i * width + j + 1]!);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && a[start + i] === b[start + j]) {
        out.push({ change: 'same', text: a[start + i]! });
        i += 1;
        j += 1;
      } else if (j >= m || (i < n && longest[(i + 1) * width + j]! >= longest[i * width + j + 1]!)) {
        out.push({ change: 'removed', text: a[start + i]! });
        i += 1;
      } else {
        out.push({ change: 'added', text: b[start + j]! });
        j += 1;
      }
    }
  }
  for (const text of a.slice(endA)) out.push({ change: 'same', text });
  return out;
}
