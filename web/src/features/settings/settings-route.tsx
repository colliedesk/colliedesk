import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, PlanLimitError } from '../../api/client';
import { EmailChannelSection } from './email-channel-section';
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
  const emailChannel = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  const saveEmail = useMutation({ mutationFn: api.saveEmailChannel, onSuccess: () => refresh('email-channel') });
  const workspaceSlug = useMutation({ mutationFn: api.updateWorkspaceSlug, onSuccess: () => { void refresh('me'); void refresh('email-channel'); } });
  const rotateSecret = useMutation({ mutationFn: api.rotateEmailSecret, onSuccess: () => refresh('email-channel') });
  const verifyDomain = useMutation({ mutationFn: api.verifyEmailDomain, onSuccess: () => refresh('email-channel') });
  const disconnectEmail = useMutation({ mutationFn: api.disconnectEmailChannel, onSuccess: () => { rotateSecret.reset(); void refresh('email-channel'); } });
  const isAdmin = me.data?.agent.role === 'admin';
  const emailFailed = saveEmail.isError || rotateSecret.isError || disconnectEmail.isError || verifyDomain.isError || workspaceSlug.isError;
  return <SettingsPage sessions={sessions.data?.items ?? []} invites={invites.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'}
    canCreateInvites={isAdmin} canManageWorkspace={isAdmin}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeInvite.isError || revokeSession.isError || emailFailed ? messages.settingsActionFailed : undefined}
    emailSection={<EmailChannelSection settings={emailChannel.data} timezone={me.data?.timezone ?? 'UTC'} canManage={isAdmin} workspaceSlug={me.data?.workspace.slug}
      onSaveSlug={(slug) => workspaceSlug.mutate(slug)} createdSecret={rotateSecret.data?.inbound_secret} onSave={(address) => saveEmail.mutate(address)}
      onRotate={() => rotateSecret.mutate()} onVerifyDomain={() => verifyDomain.mutate()} onDisconnect={() => disconnectEmail.mutate()} />}
    inviteError={createInvite.error instanceof PlanLimitError ? createInvite.error.message : createInvite.isError ? messages.settingsActionFailed : undefined}
    createdInviteUrl={createInvite.data?.url}
    onCreateInvite={(email, role) => createInvite.mutate({ email, role })}
    onRevokeInvite={(id) => revokeInvite.mutate(id)} onRevokeSession={(id) => revokeSession.mutate(id)}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutate(value)} onSetAgentTimezone={(value) => agentTimezone.mutate(value)} />;
}
