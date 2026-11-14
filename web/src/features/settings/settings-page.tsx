import type { ReactNode } from 'react';
import type { Invite, Session } from '../../api/schemas';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { DisplayNameField } from './display-name-field';
import { InvitesSection } from './invites-section';
import type { Role } from '../../api/schemas';
import { NameForm } from './name-form';
import { PreferencesSection } from './preferences-section';
import type { SaveState } from './save-feedback';
import { ConnectedAppsSection } from './connected-apps-section';
import { SessionsSection } from './sessions-section';
import { useInstantSave } from './settings-edits';
import { SettingsFrame } from './settings-frame';
import { MembersTabs } from './members-tabs';
import type { MembersTab } from './settings-anchors';
import type { InviteDraft } from './use-invite-draft';
import type { TabChange } from '../../ui/tabs';
import { TimezoneSelect } from './timezone-select';

// The settings pages drawn here; AI & agents, Data and the others are pages of their own modules.
export type SettingsPageName = 'account' | 'general' | 'members' | 'saved-replies';

interface SettingsPageProps {
  page: SettingsPageName;
  sessions?: Session[];
  invites?: Invite[];
  timezone: string;
  canCreateInvites?: boolean;
  canManageWorkspace?: boolean;
  workspaceTimezone?: string;
  workspaceName?: string;
  workspaceNameSave?: SaveState;
  onSetWorkspaceName?: (name: string) => Promise<unknown> | void;
  timezoneOverride?: string | null;
  // email and displayName are the person signed in; an empty name shows the email.
  email?: string;
  displayName?: string;
  onSetDisplayName?: (name: string) => Promise<unknown> | void;
  // The setup guide in the inbox is this agent's choice, saved with the profile.
  setupGuideHidden?: boolean;
  onSetSetupGuideHidden?: (hidden: boolean) => Promise<unknown> | void;
  displayNameSave?: SaveState;
  // The invite just created: its one-time link, or none when a repeat of the press came back without it.
  createdInvite?: { url?: string };
  creatingInvite?: boolean;
  onCreateInvite?: (email: string, roleIds: string[]) => void;
  // The roles an invite may give.
  inviteRoles?: Role[];
  // Revoking needs team:revoke.
  canRevokeInvites?: boolean;
  onRevokeSession?: (id: string) => void;
  onRevokeInvite?: (id: string) => void;
  onSetWorkspaceTimezone?: (timezone: string) => Promise<unknown> | void;
  onSetAgentTimezone?: (timezone: string | null) => Promise<unknown> | void;
  loading?: boolean;
  error?: string;
  workspaceTimezoneSave?: SaveState;
  personalTimezoneSave?: SaveState;
  // inviteError explains a refused invite, for example a full agent cap.
  inviteError?: string;
  // teamSection lists the agents; invites follow it, as they add to the same people.
  teamSection?: ReactNode;
  // The mention email setting under Preferences, which the route wires to the server.
  mentionEmail?: ReactNode;
  // rolesSection lists the workspace's roles, under the team that holds them.
  rolesSection?: ReactNode;
  // savedRepliesSection is for every agent, not only admins.
  savedRepliesSection?: ReactNode;
  // numbersSection is the conversation number series; everyone who reads conversations sees it.
  numbersSection?: ReactNode;
  // showInvites is false for a person whose roles may not read the team (team:read).
  showInvites?: boolean;
  // mcpAddress is this server's MCP address, which Connected AI apps tells a person to add in their app.
  mcpAddress?: string;
  // showBilling adds Plan and billing to the menu; only admins of a billed (cloud) workspace see it.
  showBilling?: boolean;
  // Members: the tab open, the way to change it, and what the tabs count.
  membersTab?: MembersTab;
  onMembersTab?: (tab: MembersTab, how: TabChange) => void;
  memberCounts?: { people?: number; invites?: number };
  // The invite being typed, kept by the page so a change of tab does not drop it.
  inviteDraft?: InviteDraft;
}

const titles: Record<SettingsPageName, string> = { account: messages.account, general: messages.general, members: messages.members, 'saved-replies': messages.savedReplies };

// SettingsPage draws one of the settings pages that hold the sections of the old one-page Settings: Account
// (what belongs to the person signed in), General, Members and Saved replies (what the workspace shares).
export function SettingsPage(props: SettingsPageProps) {
  const { page, timezone, canManageWorkspace = false, loading = false, error, teamSection, savedRepliesSection, numbersSection, showBilling = false, showInvites = true } = props;
  return (
    <SettingsFrame page={`/settings/${page}`} title={titles[page]} lead={settingsMessages.pageLeads[page]} loading={loading}
      shown={{ general: canManageWorkspace, conversations: page === 'general' ? Boolean(numbersSection) : undefined, team: Boolean(teamSection) || showInvites, invites: showInvites, channels: true,
        savedReplies: page === 'saved-replies' ? Boolean(savedRepliesSection) : true, exportData: canManageWorkspace, billing: showBilling, importData: canManageWorkspace, ai: true, agents: true }}>
      {error && <p role="alert" className="error-copy">{error}</p>}
      {page === 'account' && <AccountSections {...props} />}
      {page === 'general' && <>
        {canManageWorkspace && <GeneralSection {...props} />}
        {numbersSection}
      </>}
      {page === 'members' && <MembersTabs tab={props.membersTab ?? 'people'} onTab={(tab, how) => props.onMembersTab?.(tab, how)} counts={props.memberCounts ?? {}}
        people={<>{teamSection}{teamSection && props.inviteRoles && <p className="muted-copy">{props.inviteRoles?.length === 2 ? settingsMessages.defaultRolesNote : settingsMessages.customRolesNote}</p>}</>}
        invites={showInvites && <InvitesSection invites={props.invites ?? []} timezone={timezone} canCreateInvites={props.canCreateInvites ?? false} canRevokeInvites={props.canRevokeInvites} createdInvite={props.createdInvite}
          creatingInvite={props.creatingInvite} inviteError={props.inviteError} roles={props.inviteRoles} onCreateInvite={props.onCreateInvite} onRevokeInvite={props.onRevokeInvite} draft={props.inviteDraft} />}
        advanced={props.rolesSection} />}
      {page === 'saved-replies' && savedRepliesSection}
    </SettingsFrame>
  );
}

// Account: the profile, this browser's preferences, the devices signed in and the AI apps connected with OAuth.
// The agent tokens and the sessions they opened are in AI & agents.
function AccountSections({ sessions = [], timezone, displayName = '', email, displayNameSave, onSetDisplayName, timezoneOverride = null, workspaceTimezone = 'UTC',
  personalTimezoneSave, onSetAgentTimezone, setupGuideHidden = false, onSetSetupGuideHidden, onRevokeSession, mentionEmail, mcpAddress = `${window.location.origin}/mcp` }: SettingsPageProps) {
  const choose = useInstantSave();
  return <>
    <section className="settings-card" aria-labelledby="profile-heading">
      <div className="section-heading"><h2 id="profile-heading">{messages.profile}</h2><p>{settingsMessages.profileDescription}</p></div>
      <div className="settings-body settings-rows">
        <DisplayNameField value={displayName} email={email} save={displayNameSave} onSave={onSetDisplayName} />
        <TimezoneSelect label={settingsMessages.yourTimezone} value={timezoneOverride ?? ''} emptyOption={settingsMessages.sameAsWorkspace(workspaceTimezone)}
          save={personalTimezoneSave} onChange={(value) => void choose(onSetAgentTimezone ?? (() => undefined), value || null, timezoneOverride)} />
      </div>
    </section>
    <PreferencesSection setupGuideHidden={setupGuideHidden} onSetSetupGuideHidden={onSetSetupGuideHidden} mentionEmail={mentionEmail} />
    <SessionsSection sessions={sessions} timezone={timezone} onRevokeSession={onRevokeSession} />
    <ConnectedAppsSection sessions={sessions} timezone={timezone} mcpAddress={mcpAddress} onDisconnect={onRevokeSession} />
  </>;
}

function GeneralSection({ workspaceName = '', workspaceNameSave, onSetWorkspaceName, workspaceTimezone = 'UTC', workspaceTimezoneSave, onSetWorkspaceTimezone }: SettingsPageProps) {
  const choose = useInstantSave();
  return <section className="settings-card" aria-labelledby="general-heading">
    <div className="section-heading"><h2 id="general-heading" className="sr-only">{messages.general}</h2><p>{settingsMessages.generalDescription}</p></div>
    <div className="settings-body settings-rows">
      <NameForm label={messages.workspaceName} hint={settingsMessages.workspaceNameHelp}
        value={workspaceName} canManage save={workspaceNameSave} onSave={onSetWorkspaceName} minLength={2} maxLength={60} />
      <TimezoneSelect label={settingsMessages.workspaceTimezone} value={workspaceTimezone} save={workspaceTimezoneSave}
        onChange={(value) => void choose(onSetWorkspaceTimezone ?? (() => undefined), value, workspaceTimezone)} />
    </div>
  </section>;
}
