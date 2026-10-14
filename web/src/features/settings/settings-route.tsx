import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { SettingsPage } from './settings-page';
import { messages } from '../../messages';

export function SettingsRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  const createInvite = useMutation({ mutationFn: ({ email, role }: { email: string; role: 'admin' | 'agent' }) => api.createInvite(email, role), onSuccess: () => refresh('invites') });
  const revokeInvite = useMutation({ mutationFn: api.revokeInvite, onSuccess: () => refresh('invites') });
  const revokeSession = useMutation({ mutationFn: api.revokeSession, onSuccess: () => refresh('sessions') });
  const workspaceTimezone = useMutation({ mutationFn: api.updateWorkspaceTimezone, onSuccess: () => refresh('me') });
  const agentTimezone = useMutation({ mutationFn: api.updateAgentTimezone, onSuccess: () => refresh('me') });
  return <SettingsPage sessions={sessions.data?.items ?? []} invites={invites.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'}
    canCreateInvites={me.data?.agent.role === 'admin'} canManageWorkspace={me.data?.agent.role === 'admin'}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeInvite.isError || revokeSession.isError ? messages.settingsActionFailed : undefined}
    createdInviteUrl={createInvite.data?.url}
    onCreateInvite={(email, role) => createInvite.mutate({ email, role })}
    onRevokeInvite={(id) => revokeInvite.mutate(id)} onRevokeSession={(id) => revokeSession.mutate(id)}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutate(value)} onSetAgentTimezone={(value) => agentTimezone.mutate(value)} />;
}
