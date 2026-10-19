import { useEffect, useRef, useState } from 'react';
import type { InputHTMLAttributes, Ref } from 'react';

// DraftInput keeps what the person types while the address catches up: the router hands the value back a
// moment after each key, and a field fed straight from it would move the caret or drop a key. A value
// that comes back as one this field sent is an echo; any other value (Clear all, Back, a saved view)
// replaces the text. ready decides which typed values are sent (a date only once it is a whole day);
// delay waits that many milliseconds after the last key, so typing a word is one search, not one per key.
export function DraftInput({ value, onCommit, ready = () => true, delay = 0, inputRef, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange'> & {
  value: string; onCommit: (value: string) => void; ready?: (value: string) => boolean; delay?: number; inputRef?: Ref<HTMLInputElement>;
}) {
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  const [sent, setSent] = useState<string[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  // A delayed send uses the filters as they are when it fires, and is dropped when the text changed since
  // (more keys, or a new value from outside).
  const latest = useRef({ onCommit, text, value });
  useEffect(() => { latest.current = { onCommit, text, value }; }, [onCommit, text, value]);
  useEffect(() => () => clearTimeout(timer.current), []);
  if (value !== seen) {
    setSeen(value);
    const echo = sent.indexOf(value);
    if (echo >= 0) setSent(sent.slice(echo + 1));
    else { setSent([]); setText(value); }
  }
  return <input {...props} ref={inputRef} value={text} onChange={(event) => {
    const next = event.currentTarget.value;
    setText(next);
    clearTimeout(timer.current);
    if (!ready(next)) return;
    const commit = () => {
      setSent((pending) => [...pending, next]);
      latest.current.onCommit(next);
    };
    if (delay === 0) {
      if (next !== value) commit();
      return;
    }
    timer.current = setTimeout(() => { if (latest.current.text === next && latest.current.value !== next) commit(); }, delay);
  }} />;
}
