import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { can } from '../../lib/can';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { messages } from '../../messages';
import { useBillingShown } from '../billing/billing-shown';
import { RolesSection } from './roles/role-list';
import type { MembersTab } from './settings-anchors';
import { SettingsPage } from './settings-page';
import { TeamSection } from './team-section';
import { useInvites } from './use-invites';

// Settings → Members: everyone who can sign in, the invites that add people and, under Advanced, the roles they
// hold, one tab each. A person whose roles may not read the team (team:read) is asked for none of it.
export function MembersRoute({ tab, onTab }: { tab?: MembersTab; onTab?: (tab: MembersTab) => void } = {}) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const readsTeam = can(me.data, 'team:read');
  const invites = useInvites(readsTeam);
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: readsTeam });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  // A team change also moves seats, sessions and assignments, so every view that shows them reloads.
  const afterTeamChange = () => { for (const key of ['agents', 'sessions', 'invites', 'billing', 'conversations', 'me', 'roles']) void refresh(key); };
  const updateAgent = useMutation({
    mutationFn: ({ id, ...change }: { id: string; display_name?: string; active?: boolean; reassign_to?: string | null }) => api.updateAgent(id, change),
    // A rename also shows in the account menu when it is your own row.
    onSuccess: afterTeamChange,
  });
  const setRoles = useMutation({ mutationFn: ({ id, roleIds }: { id: string; roleIds: string[] }) => api.setAgentRoles(id, roleIds), onSuccess: afterTeamChange });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  const loading = me.isPending || (readsTeam && invites.invites.isPending);
  useScrollToHashWhenReady(!loading);
  const roles = invites.roles;
  const activePeople = team.data?.items.filter((member) => !member.deactivated_at).length;
  const pendingInvites = invites.section.invites.filter((invite) => !invite.used_at && !invite.revoked_at && Date.parse(invite.expires_at) > Date.now()).length;
  return <SettingsPage page="members" membersTab={tab} onMembersTab={onTab} memberCounts={{ people: activePeople, invites: readsTeam ? pendingInvites : undefined }} loading={loading} timezone={me.data?.timezone ?? 'UTC'} showBilling={billingShown} showInvites={readsTeam}
    invites={invites.section.invites} canCreateInvites={can(me.data, 'team:invite')} canRevokeInvites={can(me.data, 'team:revoke')} inviteRoles={roles.data?.items}
    inviteError={invites.section.inviteError} createdInvite={invites.section.createdInvite} creatingInvite={invites.section.creatingInvite}
    onCreateInvite={invites.section.onCreateInvite} onRevokeInvite={invites.section.onRevokeInvite}
    error={invites.revokeFailed ? messages.settingsActionFailed : undefined}
    teamSection={readsTeam ? <TeamSection members={team.data?.items ?? []} currentAgentId={me.data?.agent.id} canManage={can(me.data, 'team:manage_roles')}
      roles={roles.data?.items} myPermissions={me.data?.permissions} timezone={me.data?.timezone ?? 'UTC'}
      error={updateAgent.error instanceof PlanLimitError || updateAgent.error instanceof InvalidRequestError ? updateAgent.error.message : updateAgent.isError ? messages.settingsActionFailed : undefined}
      onSetRoles={(id, roleIds) => setRoles.mutateAsync({ id, roleIds })}
      onRename={(id, name) => updateAgent.mutate({ id, display_name: name })}
      onDeactivate={(id, reassignTo) => updateAgent.mutate({ id, active: false, ...(reassignTo ? { reassign_to: reassignTo } : {}) })}
      onReactivate={(id) => updateAgent.mutate({ id, active: true })} /> : undefined}
    rolesSection={readsTeam ? <RolesSection /> : undefined} />;
}
