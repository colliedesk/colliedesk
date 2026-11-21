import { useState } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { ArrowRightIcon, BanIcon, CheckIcon, MinusIcon, RequestsIcon, TagIcon, UserIcon, XIcon } from '../../ui/icons';
import { useHolds } from '../../lib/permissions-context';
import { ActionMenu } from '../inbox/action-menu';
import { tagOf } from './article-tags-field';
import { Icon } from './editor/icons';
import { kbApi, visibilitySchema } from './kb-api';
import type { KBBulkChange } from './kb-bulk-api';
import { kbBulkMessages as m } from './kb-bulk-messages';
import { indent } from './kb-tree';
import type { TreeNode } from './kb-tree';
import { useReviewers } from './review-send-dialog';
import { visibilityMessages } from './visibility-messages';

interface KBBulkBarProps {
  count: number;
  // all: every article the filters match is selected, not only the ticked rows.
  all: boolean;
  nodes: TreeNode[];
  meId?: string;
  busy: boolean;
  onSelectAll: () => void;
  onApply: (change: KBBulkChange) => void;
  onClear: () => void;
}

// KBBulkBar is what the person can do to the selected articles. It offers only what their role may do;
// the server still answers each article on its own and refuses what the call for it alone would refuse.
export function KBBulkBar({ count, all, nodes, meId, busy, onSelectAll, onApply, onClear }: KBBulkBarProps) {
  const holds = useHolds();
  const tags = useQuery({ queryKey: ['kb-tags'], queryFn: kbApi.tags });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: holds('team:read') });
  const { reviewers } = useReviewers(meId);
  const known = tags.data?.items.map((item) => item.tag) ?? [];
  const members = team.data?.items.filter((member) => !member.deactivated_at) ?? [];
  const publishes = holds('kb_articles:publish');
  const clearOnEscape = (event: KeyboardEvent<HTMLDivElement>) => { if (event.key === 'Escape' && !busy) onClear(); };
  const button = (label: string, icon: ReactNode, change: KBBulkChange, danger = false) => (
    <button type="button" className={danger ? 'icon-button danger' : 'icon-button'} aria-label={label} data-tip={label} disabled={busy}
      onClick={() => onApply(change)}>{icon}</button>
  );
  return (
    <div className="bulk-bar kb-bulk-bar" role="group" aria-label={m.bar} onKeyDown={clearOnEscape}>
      <strong className="bulk-count">{all ? m.allMatching : m.selected(count)}</strong>
      {!all && <button type="button" className="link-action" data-tip={m.selectAllMatchingTip} disabled={busy} onClick={onSelectAll}>{m.selectAllMatching}</button>}
      <div className="bulk-actions">
        <ActionMenu label={m.move} icon={<ArrowRightIcon />} disabled={busy} onApply={onApply}>
          {(apply) => nodes.map((node) => <button type="button" key={node.collection.id}
            onClick={() => apply({ action: 'move', collection_id: node.collection.id })}>{indent(node)}</button>)}
        </ActionMenu>
        <ActionMenu label={m.visibility} icon={<Icon name="eye" />} disabled={busy} onApply={onApply}>
          {(apply) => visibilitySchema.options.map((visibility) => <button type="button" key={visibility}
            onClick={() => apply({ action: 'visibility', visibility })}>{visibilityMessages.names[visibility]}</button>)}
        </ActionMenu>
        {members.length > 0 && <ActionMenu label={m.author} icon={<UserIcon />} disabled={busy} onApply={onApply}>
          {(apply) => members.map((member) => <button type="button" key={member.id}
            onClick={() => apply({ action: 'author', author_agent_id: member.id })}>{member.display_name || member.email}</button>)}
        </ActionMenu>}
        <ActionMenu label={m.addTag} icon={<TagIcon />} disabled={busy} onApply={onApply}>
          {(apply) => <>
            {known.map((tag) => <button type="button" key={tag} onClick={() => apply({ action: 'tags_add', tags: [tag] })}>{tag}</button>)}
            <NewTag busy={busy} onAdd={(tag) => apply({ action: 'tags_add', tags: [tag] })} />
          </>}
        </ActionMenu>
        <ActionMenu label={m.removeTag} icon={<MinusIcon />} disabled={busy} onApply={onApply}>
          {(apply) => known.length === 0 ? <p className="muted-copy">{m.noTags}</p>
            : known.map((tag) => <button type="button" key={tag} onClick={() => apply({ action: 'tags_remove', tags: [tag] })}>{tag}</button>)}
        </ActionMenu>
        <ActionMenu label={m.sendForReview} icon={<RequestsIcon />} disabled={busy} onApply={onApply}>
          {(apply) => reviewers.length === 0 ? <p className="muted-copy">{m.noReviewers}</p>
            : reviewers.map((reviewer) => <button type="button" key={reviewer.id}
              onClick={() => apply({ action: 'send_for_review', reviewer_ids: [reviewer.id] })}>{reviewer.name}</button>)}
        </ActionMenu>
        {publishes && <>
          {button(m.approve, <CheckIcon />, { action: 'approve' })}
          {button(m.publish, <Icon name="globe" />, { action: 'publish' })}
          {button(m.unpublish, <BanIcon />, { action: 'unpublish' })}
        </>}
        {holds('kb_articles:delete') && button(m.trash, <Icon name="trash" />, { action: 'trash' }, true)}
        <button type="button" className="icon-button" aria-label={m.clear} data-tip={m.clear} disabled={busy} onClick={onClear}><XIcon /></button>
      </div>
    </div>
  );
}

// NewTag adds a tag no article carries yet, written as the article editor writes tags.
function NewTag({ busy, onAdd }: { busy: boolean; onAdd: (tag: string) => void }) {
  const [text, setText] = useState('');
  const tag = tagOf(text);
  return (
    <form className="kb-bulk-new-tag" onSubmit={(event) => { event.preventDefault(); if (tag) { onAdd(tag); setText(''); } }}>
      <input type="text" aria-label={m.newTag} placeholder={m.newTag} maxLength={40} value={text} onChange={(event) => setText(event.target.value)} />
      <button type="submit" disabled={busy || !tag}>{m.add}</button>
    </form>
  );
}
