import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { useBillingShown } from '../billing/billing-shown';
import { everyPage, SettingsFrame } from './settings-frame';
import { tagMessages as m } from './tag-messages';
import { TagsSection } from './tags-section';

// Settings → Tags: every tag of the workspace with how many conversations carry it, renamed or deleted in its row.
export function TagsRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const readsTags = can(me.data, 'tags:read');
  const tags = useQuery({ queryKey: ['tags', 'counts'], queryFn: api.tagsWithCounts, enabled: readsTags });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  // A tag's name shows on every conversation that has it, so the inbox reads its conversations again too.
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['tags'] }), client.invalidateQueries({ queryKey: ['conversations'] })]);
  return <SettingsFrame page="/settings/tags" title={m.tags} lead={m.lead} loading={me.isPending || (readsTags && tags.isPending)} shown={{ ...everyPage, billing: billingShown }}>
    {tags.isError
      ? <div role="alert" className="notice"><span>{m.loadFailed}</span> <button type="button" onClick={() => void tags.refetch()}>{m.tryAgain}</button></div>
      : readsTags && <TagsSection tags={tags.data?.items ?? []} canRename={can(me.data, 'tags:update')} canDelete={can(me.data, 'tags:delete')}
        onRename={async (id, name) => { await api.renameTag(id, name); await refresh(); }}
        onDelete={async (id) => { await api.deleteTag(id); await refresh(); }} />}
  </SettingsFrame>;
}
