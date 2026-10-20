import { useEffect, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { SavedReplyPicker } from '../inbox/saved-reply-picker';
import { typingDelayMs } from '../inbox/inbox-filters';
import { kbApi } from './kb-api';
import type { KBArticleSummary } from './kb-api';
import { kbMessages } from './kb-messages';

interface ArticleLinkPickerProps {
  onPick: (article: KBArticleSummary) => void;
  onClose: (restoreFocus: boolean) => void;
  // Opens the help center settings; only for an admin, who can turn it on.
  onManageHelpCenter?: () => void;
}

// ArticleLinkPicker is the saved reply picker over the published articles, searched on the server. An
// article has a public page only while the help center is on, so while it is off the picker says why
// there is nothing to link instead of listing addresses that answer 404.
export function ArticleLinkPicker({ onPick, onClose, onManageHelpCenter }: ArticleLinkPickerProps) {
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  useEffect(() => { const timer = window.setTimeout(() => setQ(text.trim()), typingDelayMs); return () => window.clearTimeout(timer); }, [text]);
  // Opening the list asks again, past the app's 15 seconds: the help center may have been turned on in the
  // Settings tab the list itself opens, or an article published a moment ago.
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter, refetchOnMount: 'always' });
  const enabled = center.data?.enabled === true;
  const list = useQuery({
    queryKey: ['kb-articles', { q: q || undefined, status: 'published', for: 'reply' }],
    queryFn: ({ signal }) => kbApi.articles({ q: q || undefined, status: 'published' }, signal),
    enabled, placeholderData: keepPreviousData, refetchOnMount: 'always',
  });
  const state = center.isError || list.isError ? 'failed' : center.isPending || (enabled && list.isPending) ? 'loading' : 'ready';
  const notice = center.data && !enabled && (
    <p className="saved-reply-empty" role="status">{kbMessages.linkHelpCenterOff}{' '}
      {onManageHelpCenter ? <button type="button" className="link-action" onClick={onManageHelpCenter}>{kbMessages.linkOpenSettings}</button> : kbMessages.linkHelpCenterAdmins}</p>
  );
  return <SavedReplyPicker replies={enabled ? list.data?.items ?? [] : []} onPick={onPick} onClose={onClose} state={state} texts={kbMessages.linkPicker}
    onQueryChange={setText} notice={notice} />;
}
