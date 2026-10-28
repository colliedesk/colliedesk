import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, PlanLimitError } from '../../api/client';
import { AnsweredWithoutSecret, useCreateMutation } from '../../api/use-create';
import { AgentTokensSection } from './agent-tokens-section';
import { ExportAllData } from './export-all-data';
import { SavedRepliesSection } from './saved-replies-section';
import { TeamSection } from './team-section';
import { SettingsPage } from './settings-page';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { Part, PartUnavailable } from '../../ui/lazy-part';
import { helpCenterPart } from '../kb/kb-part';
import type { AgentToken } from '../../api/schemas';
import type { TokenChoice } from './agent-tokens-section';
import { saveState } from './save-feedback';

// The email channel and the web form were sections of this page; a link to their old anchors (a bookmark,
// an old guide) opens their channel instead. Rare, so it is done here, in this page's own chunk.
const movedSections: Record<string, string> = { '#email-channel-heading': '/settings/channels/type/email', '#web-form-heading': '/settings/channels/type/web_form' };

export function SettingsRoute() {
  useEffect(() => {
    const follow = () => {
      const moved = movedSections[window.location.hash];
      if (moved) window.location.replace(moved);
    };
    follow();
    // An old anchor opened while Settings is already on screen changes only the hash.
    window.addEventListener('hashchange', follow);
    return () => window.removeEventListener('hashchange', follow);
  }, []);
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions });
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const refresh = (key: string) => client.invalidateQueries({ queryKey: [key] });
  const createInvite = useCreateMutation({ create: ({ email, role }: { email: string; role: 'admin' | 'agent' }, key: string) => api.createInvite(email, role, key), onSuccess: () => refresh('invites') });
  // A team change also moves seats, sessions and assignments, so every view that shows them reloads.
  const updateAgent = useMutation({
    mutationFn: ({ id, ...change }: { id: string; display_name?: string; role?: 'admin' | 'agent'; active?: boolean; reassign_to?: string | null }) => api.updateAgent(id, change),
    // A rename also shows in the account menu when it is your own row.
    onSuccess: () => { for (const key of ['agents', 'sessions', 'invites', 'billing', 'conversations', 'me']) void refresh(key); },
  });
  // A mutation, so that a 401 or 402 signs out or locks the app as any action does. The new token is in no
  // cache: keep hands it to the card, and the mutation's own result is empty. Sessions reloads to list it,
  // also after a failure whose outcome is unknown, so a token the server did create shows up to be revoked.
  // A repeat of a press the server already answered comes back without the token (shown once): the card
  // then points to Sessions, where the token is listed to revoke.
  const createToken = useCreateMutation({
    create: async ({ label, choice, keep }: { label: string; choice: TokenChoice; keep: (created: AgentToken & { token: string }) => void }, key: string) => {
      const created = await api.createAgentToken(label, choice, key);
      if (!created.token) throw new AnsweredWithoutSecret();
      keep({ ...created, token: created.token });
    },
    onSettled: () => refresh('sessions'),
  });
  const revokeInvite = useMutation({ mutationFn: api.revokeInvite, onSuccess: () => refresh('invites') });
  // Revoking this device's own session signs it out; reloading me shows the signed-out screen.
  const revokeSession = useMutation({ mutationFn: api.revokeSession, onSuccess: () => { void refresh('sessions'); void refresh('me'); } });
  const workspaceTimezone = useMutation({ mutationFn: api.updateWorkspaceTimezone, onSuccess: () => refresh('me') });
  // The name shows in the workspace switcher and the header, which read it from me.
  const workspaceName = useMutation({ mutationFn: (name: string) => api.updateWorkspaceName(name), onSuccess: () => refresh('me') });
  const agentTimezone = useMutation({ mutationFn: api.updateAgentTimezone, onSuccess: () => refresh('me') });
  // Your name also shows in the team list and the assignee lists.
  const agentDisplayName = useMutation({ mutationFn: api.updateAgentDisplayName, onSuccess: () => { void refresh('me'); void refresh('agents'); } });
  // The help center card is its own chunk (the knowledge base's); it loads alongside the settings requests.
  useEffect(helpCenterPart.preload, []);
  const savedReplies = useQuery({ queryKey: ['saved-replies'], queryFn: api.savedReplies });
  const refreshSavedReplies = () => refresh('saved-replies');
  // A saved reply added again after a failed request goes under the same key, so it is stored once.
  const createSavedReply = useCreateMutation({ create: ({ title, body }: { title: string; body: string }, key: string) => api.createSavedReply(title, body, key),
    onSuccess: refreshSavedReplies });
  const updateSavedReply = useMutation({ mutationFn: ({ id, ...change }: { id: string; title: string; body: string; updated_at: string }) => api.updateSavedReply(id, change), onSuccess: refreshSavedReplies });
  const deleteSavedReply = useMutation({ mutationFn: ({ id, updatedAt }: { id: string; updatedAt: string }) => api.deleteSavedReply(id, updatedAt), onSuccess: refreshSavedReplies });
  const isAdmin = me.data?.agent.role === 'admin';
  // Plan and billing exists only on the hosted cloud and only admins manage it.
  const showBilling = isAdmin && config.data?.billing.enabled === true;
  const loading = me.isPending || sessions.isPending || invites.isPending;
  // A link to a section (the nav from another settings page, a pasted address) arrives before the sections do.
  useScrollToHashWhenReady(!loading);
  return <SettingsPage loading={loading} sessions={sessions.data?.items ?? []} invites={invites.data?.items ?? []} timezone={me.data?.timezone ?? 'UTC'}
    canCreateInvites={isAdmin} canManageWorkspace={isAdmin} showBilling={showBilling} exportSection={isAdmin ? <ExportAllData /> : undefined}
    workspaceTimezoneSave={saveState(workspaceTimezone)} personalTimezoneSave={saveState(agentTimezone)}
    workspaceName={me.data?.workspace.name} workspaceNameSave={saveState(workspaceName)} onSetWorkspaceName={(name) => workspaceName.mutate(name)}
    email={me.data?.agent.email} displayName={me.data?.agent.display_name} displayNameSave={saveState(agentDisplayName)}
    onSetDisplayName={(name) => agentDisplayName.mutate(name)}
    workspaceTimezone={me.data?.workspace.timezone} timezoneOverride={me.data?.agent.timezone_override}
    error={revokeInvite.isError || revokeSession.isError ? messages.settingsActionFailed : undefined}
    agentsSection={<AgentTokensSection canCreateAdmin={isAdmin}
      onCreate={(label, choice) => new Promise((keep, reject) => { createToken.mutateAsync({ label, choice, keep }).catch(reject); })} />}
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
    helpCenterSection={<section className="settings-card" aria-labelledby="help-center-heading">
      <div className="section-heading"><h2 id="help-center-heading">{messages.helpCenter}</h2><p>{settingsMessages.helpCenterDescription}</p></div>
      <div className="settings-body"><Part part={helpCenterPart} failed={<PartUnavailable />}
        render={({ HelpCenterSettings }) => <HelpCenterSettings canManage={isAdmin} workspaceName={me.data?.workspace.name} timezone={me.data?.timezone ?? 'UTC'} />} /></div>
    </section>}
    inviteError={createInvite.error instanceof PlanLimitError || createInvite.error instanceof InvalidRequestError ? createInvite.error.message : createInvite.isError ? messages.settingsActionFailed : undefined}
    createdInvite={createInvite.data ? { url: createInvite.data.url } : undefined} creatingInvite={createInvite.isPending}
    onCreateInvite={(email, role) => createInvite.mutate({ email, role })}
    onRevokeInvite={(id) => revokeInvite.mutate(id)} onRevokeSession={(id) => revokeSession.mutate(id)}
    onSetWorkspaceTimezone={(value) => workspaceTimezone.mutate(value)} onSetAgentTimezone={(value) => agentTimezone.mutate(value)} />;
}
