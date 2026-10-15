import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { ExportAllData } from './export-all-data';
import { EmailChannelSection } from './email-channel-section';
import { TeamSection } from './team-section';
import { WebFormSection } from './web-form-section';
import { SettingsPage } from './settings-page';
import { messages } from '../../messages';

export function SettingsRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  const createInvite = useMutation({ mutationFn: ({ email, role }: { email: string; role: 'admin' | 'agent' }) => api.createInvite(email, role), onSuccess: () => refresh('invites') });
  // A team change also moves seats, sessions and assignments, so every view that shows them reloads.
  const updateAgent = useMutation({
    mutationFn: ({ id, ...change }: { id: string; role?: 'admin' | 'agent'; active?: boolean; reassign_to?: string | null }) => api.updateAgent(id, change),
    onSuccess: () => { for (const key of ['agents', 'sessions', 'invites', 'billing', 'conversations']) void refresh(key); },
  });
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
  const webForm = useQuery({ queryKey: ['web-form'], queryFn: api.webForm });
  const saveWebForm = useMutation({ mutationFn: ({ enabled, origins }: { enabled: boolean; origins: string[] }) => api.saveWebForm(enabled, origins), onSuccess: () => refresh('web-form') });
  const isAdmin = me.data?.agent.role === 'admin';
  const emailFailed = saveEmail.isError || rotateSecret.isError || disconnectEmail.isError || verifyDomain.isError || workspaceSlug.isError;
  return <SettingsPage sessions={sessions.data?.items ?? []} invites={invites.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'}
    canCreateInvites={isAdmin} canManageWorkspace={isAdmin} exportSection={isAdmin ? <ExportAllData /> : undefined}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeInvite.isError || revokeSession.isError || emailFailed ? messages.settingsActionFailed : undefined}
    teamSection={<TeamSection members={team.data?.items ?? []} currentAgentId={me.data?.agent.id} canManage={isAdmin} timezone={me.data?.timezone ?? 'UTC'}
      error={updateAgent.error instanceof PlanLimitError || updateAgent.error instanceof InvalidRequestError ? updateAgent.error.message : updateAgent.isError ? messages.settingsActionFailed : undefined}
      onChangeRole={(id, role) => updateAgent.mutate({ id, role })}
      onDeactivate={(id, reassignTo) => updateAgent.mutate({ id, active: false, ...(reassignTo ? { reassign_to: reassignTo } : {}) })}
      onReactivate={(id) => updateAgent.mutate({ id, active: true })} />}
    emailSection={<EmailChannelSection settings={emailChannel.data} timezone={me.data?.timezone ?? 'UTC'} canManage={isAdmin} workspaceSlug={me.data?.workspace.slug}
      onSaveSlug={(slug) => workspaceSlug.mutate(slug)} createdSecret={rotateSecret.data?.inbound_secret} onSave={(address) => saveEmail.mutate(address)}
      onRotate={() => rotateSecret.mutate()} onVerifyDomain={() => verifyDomain.mutate()} onDisconnect={() => disconnectEmail.mutate()} />}
    webFormSection={<WebFormSection settings={webForm.data} canManage={isAdmin} error={saveWebForm.isError ? (saveWebForm.error instanceof InvalidRequestError ? saveWebForm.error.message : messages.settingsActionFailed) : undefined} onSave={(enabled, origins) => saveWebForm.mutate({ enabled, origins })} />}
    inviteError={createInvite.error instanceof PlanLimitError || createInvite.error instanceof InvalidRequestError ? createInvite.error.message : createInvite.isError ? messages.settingsActionFailed : undefined}
    createdInviteUrl={createInvite.data?.url}
    onCreateInvite={(email, role) => createInvite.mutate({ email, role })}
    onRevokeInvite={(id) => revokeInvite.mutate(id)} onRevokeSession={(id) => revokeSession.mutate(id)}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutate(value)} onSetAgentTimezone={(value) => agentTimezone.mutate(value)} />;
}
