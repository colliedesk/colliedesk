import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { useBillingShown } from '../billing/billing-shown';
import { NumbersSection } from './numbers/numbers-section';
import { saveState } from './save-feedback';
import { SettingsPage } from './settings-page';

// Settings → General: the workspace's name and timezone, and how conversations are numbered. The help center's
// settings are in Knowledge base → Settings.
export function GeneralRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  const workspaceTimezone = useMutation({ mutationFn: api.updateWorkspaceTimezone, onSuccess: () => refresh('me') });
  // The name shows in the workspace switcher and the header, which read it from me.
  const workspaceName = useMutation({ mutationFn: (name: string) => api.updateWorkspaceName(name), onSuccess: () => refresh('me') });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  useScrollToHashWhenReady(!me.isPending);
  return <SettingsPage page="general" loading={me.isPending} timezone={me.data?.timezone ?? 'UTC'} canManageWorkspace={can(me.data, 'workspace:update')} showBilling={billingShown}
    workspaceTimezoneSave={saveState(workspaceTimezone)} workspaceName={me.data?.workspace.name} workspaceNameSave={saveState(workspaceName)}
    onSetWorkspaceName={(name) => workspaceName.mutateAsync(name)} workspaceTimezone={me.data?.workspace.timezone}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutateAsync(value)}
    numbersSection={can(me.data, 'conversations:read') ? <NumbersSection /> : undefined} />;
}
