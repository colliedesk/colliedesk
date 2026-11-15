import type { KeyboardEvent, Ref } from 'react';
import { messages } from '../../messages';
import { SearchIcon, XIcon } from '../../ui/icons';
import { DraftInput } from './draft-input';

interface ListSearchFieldProps {
  inputRef: Ref<HTMLInputElement>;
  // The words in the address; the field keeps what is typed meanwhile (DraftInput).
  value: string;
  delay: number;
  onCommit: (value: string) => void;
  onFocus: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void;
  onClear: () => void;
  // Where the search looks, as the title named it: "in Zalo OA", "in all channels".
  scope: string;
  autoFocus?: boolean;
}

// ListSearchField takes the title's place while the agent searches: the words, where they are searched, and a way
// to clear them that brings the title back.
export function ListSearchField({ inputRef, value, delay, onCommit, onFocus, onKeyDown, onClear, scope, autoFocus = false }: ListSearchFieldProps) {
  return <div className="list-search">
    <SearchIcon />
    <DraftInput inputRef={inputRef} name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.search} maxLength={200} autoFocus={autoFocus}
      value={value} delay={delay} onCommit={onCommit} onFocus={onFocus} onKeyDown={onKeyDown} />
    <span className="list-search-scope">{scope}</span>
    <button type="button" className="list-search-clear" aria-label={messages.clearSearch} data-tip={messages.clearSearch} onClick={onClear}><XIcon /></button>
  </div>;
}
