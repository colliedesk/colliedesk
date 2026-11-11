import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import { can } from '../../lib/can';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { useBillingShown } from '../billing/billing-shown';
import { SavedRepliesSection } from './saved-replies-section';
import { SettingsPage } from './settings-page';

// Settings → Saved replies: the text the whole team inserts into replies; every agent adds, edits and deletes them.
export function SavedRepliesRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const readsSavedReplies = can(me.data, 'saved_replies:read');
  const savedReplies = useQuery({ queryKey: ['saved-replies'], queryFn: api.savedReplies, enabled: readsSavedReplies });
  const refreshSavedReplies = () => client.invalidateQueries({ queryKey: ['saved-replies'] });
  // A saved reply added again after a failed request goes under the same key, so it is stored once.
  const createSavedReply = useCreateMutation({ create: ({ title, body }: { title: string; body: string }, key: string) => api.createSavedReply(title, body, key),
    onSuccess: refreshSavedReplies });
  const updateSavedReply = useMutation({ mutationFn: ({ id, ...change }: { id: string; title: string; body: string; updated_at: string }) => api.updateSavedReply(id, change), onSuccess: refreshSavedReplies });
  const deleteSavedReply = useMutation({ mutationFn: ({ id, updatedAt }: { id: string; updatedAt: string }) => api.deleteSavedReply(id, updatedAt), onSuccess: refreshSavedReplies });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  useScrollToHashWhenReady(!me.isPending);
  const timezone = me.data?.timezone ?? 'UTC';
  return <SettingsPage page="saved-replies" loading={me.isPending} timezone={timezone} showBilling={billingShown}
    savedRepliesSection={readsSavedReplies && <SavedRepliesSection replies={savedReplies.data?.items ?? []}
      onCreate={(title, body) => createSavedReply.mutateAsync({ title, body })}
      onUpdate={(id, change) => updateSavedReply.mutateAsync({ id, ...change })}
      onDelete={(id, updatedAt) => deleteSavedReply.mutateAsync({ id, updatedAt })} onReload={() => void refreshSavedReplies()} timezone={timezone}
      loadHistory={(id, before) => api.savedReplyVersions(id, before)}
      onRestoreVersion={async (id, version, updatedAt) => { await api.restoreSavedReplyVersion(id, version, updatedAt); await refreshSavedReplies(); }}
      loadTrash={(cursor) => api.trashedSavedReplies(cursor)}
      onUndelete={async (id, updatedAt) => { await api.restoreSavedReply(id, updatedAt); await refreshSavedReplies(); }} />} />;
}
