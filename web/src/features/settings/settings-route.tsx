import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { ExportAllData } from './export-all-data';
import { EmailChannelSection, gmailPart } from './email-channel-section';
import { gmailReturn } from './gmail-return';
import { SavedRepliesSection } from './saved-replies-section';
import { TeamSection } from './team-section';
import { WebFormSection } from './web-form-section';
import { SettingsPage } from './settings-page';
import { messages } from '../../messages';
import { Part, PartUnavailable } from '../../ui/lazy-part';
import { helpCenterPart } from '../kb/kb-part';
import type { SaveState } from './save-feedback';

function saveState(m: { isPending: boolean; isSuccess: boolean; isError: boolean; error: unknown }): SaveState {
  return { pending: m.isPending, saved: m.isSuccess, error: m.isError ? (m.error instanceof InvalidRequestError ? m.error.message : messages.settingsActionFailed) : undefined };
}

export function SettingsRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  const createInvite = useMutation({ mutationFn: ({ email, role }: { email: string; role: 'admin' | 'agent' }) => api.createInvite(email, role), onSuccess: () => refresh('invites') });
  // A team change also moves seats, sessions and assignments, so every view that shows them reloads.
  const updateAgent = useMutation({
    mutationFn: ({ id, ...change }: { id: string; display_name?: string; role?: 'admin' | 'agent'; active?: boolean; reassign_to?: string | null }) => api.updateAgent(id, change),
    // A rename also shows in the account menu when it is your own row.
    onSuccess: () => { for (const key of ['agents', 'sessions', 'invites', 'billing', 'conversations', 'me']) void refresh(key); },
  });
  const revokeInvite = useMutation({ mutationFn: api.revokeInvite, onSuccess: () => refresh('invites') });
  // Revoking this device's own session signs it out; reloading me shows the signed-out screen.
  const revokeSession = useMutation({ mutationFn: api.revokeSession, onSuccess: () => { void refresh('sessions'); void refresh('me'); } });
  const workspaceTimezone = useMutation({ mutationFn: api.updateWorkspaceTimezone, onSuccess: () => refresh('me') });
  const agentTimezone = useMutation({ mutationFn: api.updateAgentTimezone, onSuccess: () => refresh('me') });
  // Your name also shows in the team list and the assignee lists.
  const agentDisplayName = useMutation({ mutationFn: api.updateAgentDisplayName, onSuccess: () => { void refresh('me'); void refresh('agents'); } });
  const emailChannel = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  const saveEmail = useMutation({ mutationFn: (address: string) => api.saveEmailChannel(address), onSuccess: () => refresh('email-channel') });
  const saveSenderName = useMutation({ mutationFn: ({ address, name }: { address: string; name: string }) => api.saveEmailChannel(address, name), onSuccess: () => refresh('email-channel') });
  const workspaceSlug = useMutation({ mutationFn: api.updateWorkspaceSlug, onSuccess: () => { void refresh('me'); void refresh('email-channel'); } });
  const rotateSecret = useMutation({ mutationFn: api.rotateEmailSecret, onSuccess: () => refresh('email-channel') });
  // A refused check still reloads: the provider's per-record status changed.
  const verifyDomain = useMutation({ mutationFn: api.verifyEmailDomain, onSettled: () => refresh('email-channel') });
  const disconnectEmail = useMutation({ mutationFn: api.disconnectEmailChannel, onSuccess: () => { rotateSecret.reset(); void refresh('email-channel'); } });
  // Connecting Gmail leaves for Google's consent page; Google sends the browser back with the outcome.
  const [gmailReturned] = useState(gmailReturn);
  // The "Send from Gmail" block is its own chunk; it loads alongside the settings requests, not after them.
  useEffect(gmailPart.preload, []);
  // So is the help center card, from the knowledge base chunk.
  useEffect(helpCenterPart.preload, []);
  const connectGmail = useMutation({ mutationFn: api.connectGmail, onSuccess: ({ url }) => window.location.assign(url) });
  const disconnectGmail = useMutation({ mutationFn: api.disconnectGmail, onSettled: () => refresh('email-channel') });
  const gmailFailure = connectGmail.error ?? disconnectGmail.error;
  const gmailError = gmailFailure ? (gmailFailure instanceof InvalidRequestError ? gmailFailure.message : messages.settingsActionFailed) : undefined;
  const webForm = useQuery({ queryKey: ['web-form'], queryFn: api.webForm });
  const saveWebForm = useMutation({ mutationFn: ({ enabled, origins }: { enabled: boolean; origins: string[] }) => api.saveWebForm(enabled, origins), onSuccess: () => refresh('web-form') });
  const savedReplies = useQuery({ queryKey: ['saved-replies'], queryFn: api.savedReplies });
  const refreshSavedReplies = () => refresh('saved-replies');
  const createSavedReply = useMutation({ mutationFn: ({ title, body }: { title: string; body: string }) => api.createSavedReply(title, body), onSuccess: refreshSavedReplies });
  const updateSavedReply = useMutation({ mutationFn: ({ id, ...change }: { id: string; title: string; body: string; updated_at: string }) => api.updateSavedReply(id, change), onSuccess: refreshSavedReplies });
  const deleteSavedReply = useMutation({ mutationFn: ({ id, updatedAt }: { id: string; updatedAt: string }) => api.deleteSavedReply(id, updatedAt), onSuccess: refreshSavedReplies });
  const isAdmin = me.data?.agent.role === 'admin';
  // Plan and billing exists only on the hosted cloud and only admins manage it.
  const showBilling = isAdmin && config.data?.billing.enabled === true;
  const emailFailed = rotateSecret.isError || disconnectEmail.isError || (verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError));
  return <SettingsPage loading={me.isPending || sessions.isPending || invites.isPending} sessions={sessions.data?.items ?? []} invites={invites.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'}
    canCreateInvites={isAdmin} canManageWorkspace={isAdmin} showBilling={showBilling} exportSection={isAdmin ? <ExportAllData /> : undefined}
    workspaceTimezoneSave={saveState(workspaceTimezone)} personalTimezoneSave={saveState(agentTimezone)}
    email={me.data?.agent.email} displayName={me.data?.agent.display_name} displayNameSave={saveState(agentDisplayName)}
    onSetDisplayName={(name) => agentDisplayName.mutate(name)}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeInvite.isError || revokeSession.isError || emailFailed ? messages.settingsActionFailed : undefined}
    teamSection={<TeamSection members={team.data?.items ?? []} currentAgentId={me.data?.agent.id} canManage={isAdmin} timezone={me.data?.timezone ?? 'UTC'}
      error={updateAgent.error instanceof PlanLimitError || updateAgent.error instanceof InvalidRequestError ? updateAgent.error.message : updateAgent.isError ? messages.settingsActionFailed : undefined}
      onChangeRole={(id, role) => updateAgent.mutate({ id, role })}
      onRename={(id, name) => updateAgent.mutate({ id, display_name: name })}
      onDeactivate={(id, reassignTo) => updateAgent.mutate({ id, active: false, ...(reassignTo ? { reassign_to: reassignTo } : {}) })}
      onReactivate={(id) => updateAgent.mutate({ id, active: true })} />}
    savedRepliesSection={<SavedRepliesSection replies={savedReplies.data?.items ?? []}
      onCreate={(title, body) => createSavedReply.mutateAsync({ title, body })}
      onUpdate={(id, change) => updateSavedReply.mutateAsync({ id, ...change })}
      onDelete={(id, updatedAt) => deleteSavedReply.mutateAsync({ id, updatedAt })} onReload={() => void refreshSavedReplies()} timezone={me.data?.timezone ?? 'UTC'}
      loadHistory={(id, before) => api.savedReplyVersions(id, before)}
      onRestoreVersion={async (id, version, updatedAt) => { await api.restoreSavedReplyVersion(id, version, updatedAt); await refreshSavedReplies(); }}
      loadTrash={(cursor) => api.trashedSavedReplies(cursor)}
      onUndelete={async (id, updatedAt) => { await api.restoreSavedReply(id, updatedAt); await refreshSavedReplies(); }} />}
    emailSection={<EmailChannelSection settings={emailChannel.data} timezone={me.data?.timezone ?? 'UTC'} canManage={isAdmin} workspaceSlug={me.data?.workspace.slug} addressSave={saveState(saveEmail)} senderNameSave={saveState(saveSenderName)} slugSave={saveState(workspaceSlug)}
      onSaveSlug={(slug) => workspaceSlug.mutate(slug)} createdSecret={rotateSecret.data?.inbound_secret} onSave={(address) => saveEmail.mutate(address)}
      onSaveSenderName={(name) => { const address = emailChannel.data?.channel?.support_address; if (address) saveSenderName.mutate({ address, name }); }}
      onRotate={() => rotateSecret.mutate()} onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending} verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined} onDisconnect={() => disconnectEmail.mutate()}
      onConnectGmail={() => connectGmail.mutate()} onDisconnectGmail={() => disconnectGmail.mutate()}
      gmailPending={connectGmail.isPending || connectGmail.isSuccess} gmailError={gmailError} gmailReturned={gmailReturned} />}
    helpCenterSection={<section className="settings-card" aria-labelledby="help-center-heading">
      <div className="section-heading"><h2 id="help-center-heading">{messages.helpCenter}</h2><p>{messages.helpCenterDescription}</p></div>
      <div className="settings-body"><Part part={helpCenterPart} failed={<PartUnavailable />}
        render={({ HelpCenterSettings }) => <HelpCenterSettings canManage={isAdmin} workspaceName={me.data?.workspace.name} />} /></div>
    </section>}
    webFormSection={<WebFormSection settings={webForm.data} canManage={isAdmin} save={saveState(saveWebForm)} onSave={(enabled, origins) => saveWebForm.mutate({ enabled, origins })} />}
    inviteError={createInvite.error instanceof PlanLimitError || createInvite.error instanceof InvalidRequestError ? createInvite.error.message : createInvite.isError ? messages.settingsActionFailed : undefined}
    createdInviteUrl={createInvite.data?.url}
    onCreateInvite={(email, role) => createInvite.mutate({ email, role })}
    onRevokeInvite={(id) => revokeInvite.mutate(id)} onRevokeSession={(id) => revokeSession.mutate(id)}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutate(value)} onSetAgentTimezone={(value) => agentTimezone.mutate(value)} />;
}
