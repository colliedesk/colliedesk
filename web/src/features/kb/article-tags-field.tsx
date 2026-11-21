import { useId, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { kbApi } from './kb-api';

// The article's tags in the editor's side column. They are part of the draft: Save sends them with the text,
// and a published article's new tags wait with its other unpublished changes.

export const maxTags = 20;
export const maxTagLength = 40;

export const tagMessages = {
  heading: 'Tags',
  add: 'Add a tag',
  remove: (tag: string) => `Remove tag ${tag}`,
  tooLong: `A tag is at most ${maxTagLength} characters.`,
  full: `${maxTags} tags is the most an article can have.`,
  help: 'Tags group articles in the list and help search. Readers see them only when the help center shows tags (Knowledge settings).',
};

// tagOf writes a tag as the server stores it: trimmed, lower case, single spaces.
export const tagOf = (text: string) => text.trim().toLowerCase().replace(/\s+/g, ' ');
const sorted = (tags: string[]) => [...tags].sort();
// sameTags: the server keeps tags sorted, so two lists are the same set whatever their order.
export const sameTags = (a: string[], b: string[]) => {
  const left = sorted(a);
  const right = sorted(b);
  return left.length === right.length && left.every((tag, at) => tag === right[at]);
};

interface ArticleTagsFieldProps {
  headingId: string;
  value: string[];
  onChange: (tags: string[]) => void;
}

export function ArticleTagsField({ headingId, value, onChange }: ArticleTagsFieldProps) {
  const id = useId();
  const [text, setText] = useState('');
  const [problem, setProblem] = useState<string>();
  // The workspace's tags are offered as suggestions once the field is used, not on every editor load.
  const [focused, setFocused] = useState(false);
  const known = useQuery({ queryKey: ['kb-tags'], queryFn: kbApi.tags, enabled: focused });
  const full = value.length >= maxTags;
  const add = () => {
    const tag = tagOf(text);
    if (!tag) return;
    if ([...tag].length > maxTagLength) { setProblem(tagMessages.tooLong); return; }
    setProblem(undefined);
    setText('');
    if (!value.includes(tag)) onChange(sorted([...value, tag]));
  };
  const offered = (known.data?.items ?? []).map((item) => item.tag).filter((tag) => !value.includes(tag));
  return (
    <section className="kb-side-box" aria-labelledby={headingId}>
      <h3 id={headingId}>{tagMessages.heading}</h3>
      {value.length > 0 && <ul className="kb-tag-chips" aria-label={tagMessages.heading}>
        {value.map((tag) => <li key={tag}><span>{tag}</span>
          <button type="button" aria-label={tagMessages.remove(tag)} onClick={() => onChange(value.filter((item) => item !== tag))}><span aria-hidden="true">×</span></button></li>)}
      </ul>}
      <input type="text" list={`${id}-known`} aria-label={tagMessages.add} placeholder={tagMessages.add} value={text} disabled={full}
        aria-describedby={`${id}-help`} aria-invalid={problem ? true : undefined}
        onFocus={() => setFocused(true)} onChange={(event) => { setProblem(undefined); setText(event.target.value); }}
        onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ',') { event.preventDefault(); add(); } }} />
      <datalist id={`${id}-known`}>{offered.map((tag) => <option key={tag} value={tag} />)}</datalist>
      {problem && <small role="alert" className="error-copy">{problem}</small>}
      <small id={`${id}-help`} className="kb-hint">{full ? tagMessages.full : tagMessages.help}</small>
    </section>
  );
}
