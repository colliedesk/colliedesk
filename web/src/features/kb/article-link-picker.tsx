import { useEffect, useState } from 'react';
import type { RefObject } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { SavedReplyPicker } from '../inbox/saved-reply-picker';
import { typingDelayMs } from '../inbox/inbox-filters';
import { kbApi } from './kb-api';
import type { KBArticleSummary } from './kb-api';
import { kbMessages } from './kb-messages';

interface ArticleLinkPickerProps {
  // The reply box, read for where its cursor is.
  box: RefObject<HTMLTextAreaElement | null>;
  // Puts text in the box at the cursor, as a saved reply goes in.
  onInsert: (text: string) => void;
  onClose: (restoreFocus: boolean) => void;
  // An admin can turn the help center on: the picker leads to its settings.
  canManage?: boolean;
}

// articleLink is what goes into a reply. Mail goes out as plain text, so it is the title and the address
// on two lines, not Markdown. Both get lines of their own: text right after the address would become
// part of it in the customer's mail.
export function articleLink(article: Pick<KBArticleSummary, 'title' | 'url'>, text: string, at: number): string {
  const before = at > 0 && text[at - 1] !== '\n' ? '\n' : '';
  const after = at < text.length && text[at] !== '\n' ? '\n' : '';
  return `${before}${article.title}\n${article.url}${after}`;
}

// ArticleLinkPicker is the saved reply picker over the published articles, searched on the server. An
// article has a public page only while the help center is on, so while it is off the picker says why
// there is nothing to link instead of listing addresses that answer 404. All of it is in this chunk; the
// reply box holds only the button.
// The settings open in a new tab, so the draft and the files in the reply box stay where they are.
const openHelpCenterSettings = () => { window.open('/settings#help-center-heading', '_blank', 'noopener'); };

export function ArticleLinkPicker({ box, onInsert, onClose, canManage = false }: ArticleLinkPickerProps) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  // Enter pressed before the list answered what is in the search box, kept until it has.
  const [queued, setQueued] = useState(false);
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), typingDelayMs); return () => window.clearTimeout(timer); }, [text]);
  // Opening the list asks again, past the app's 15 seconds: the help center may have been turned on in the
  // Settings tab the list itself opens, or an article published a moment ago.
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter, refetchOnMount: 'always' });
  const enabled = center.data?.enabled === true;
  // Known to be off, not merely unanswered yet (or last seen off and being asked again): what is typed
  // meanwhile is the search once it answers.
  const off = center.data?.enabled === false && !center.isFetching;
  // The list is asked for beside the help center, not after it; only a help center known to be off skips it.
  const list = useQuery({
    queryKey: ['kb-articles', { q: q || undefined, status: 'published', for: 'reply' }],
    queryFn: ({ signal }) => kbApi.articles({ q: q || undefined, status: 'published' }, signal),
    enabled: !off, placeholderData: keepPreviousData, refetchOnMount: 'always',
  });
  const items = enabled ? list.data?.items ?? [] : [];
  // Until the list answers what is in the search box, it shows the answer to the search before.
  // Until the help center answered (or answered again) nothing is offered yet, so Enter waits for that too.
  const pending = !off && (!enabled || text.trim() !== q || list.isPending || list.isPlaceholderData);
  const insert = (article: KBArticleSummary) => {
    const field = box.current;
    const value = field?.value ?? '';
    onInsert(articleLink(article, value, Math.min(field?.selectionStart ?? value.length, value.length)));
  };
  useEffect(() => {
    if (!queued || pending) return;
    setQueued(false);
    if (items[0]) insert(items[0]);
  }, [queued, pending, items, insert]);
  const state = center.isError || (enabled && list.isError) ? 'failed' : (!enabled && !off) || (enabled && list.isPending) ? 'loading' : 'ready';
  // While it is off, the list's empty state says why; an admin's way out is the picker's own button.
  const texts = off
    ? { ...kbMessages.linkPicker, none: canManage ? kbMessages.linkHelpCenterOff : kbMessages.linkHelpCenterAdmins }
    : kbMessages.linkPicker;
  return (
    // Enter on the list from before would link an article nobody searched for: it waits for the answer.
    <div className="article-link-picker" onKeyDownCapture={(event) => {
      // The Enter that commits an input method's text (Safari: keyCode 229) is the picker's to ignore.
      if (event.key !== 'Enter' || !pending || event.nativeEvent.isComposing || event.keyCode === 229) return;
      event.preventDefault();
      event.stopPropagation();
      setQueued(true);
    }}>
      <SavedReplyPicker replies={items} onPick={insert} onClose={onClose} state={state} texts={texts}
        onQueryChange={off ? undefined : (value) => { setText(value); setQueued(false); }} onManage={off && canManage ? openHelpCenterSettings : undefined}
        detail={(article) => article.url.replace(/^https?:\/\//, '')} />
    </div>
  );
}
