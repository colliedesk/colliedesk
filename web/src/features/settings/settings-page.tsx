import type { ReactNode } from 'react';
import type { Invite, Session } from '../../api/schemas';
import { messages } from '../../messages';
import { DisplayNameField } from './display-name-field';
import { InvitesSection } from './invites-section';
import { NameForm } from './name-form';
import { PreferencesSection } from './preferences-section';
import type { SaveState } from './save-feedback';
import { SessionsSection } from './sessions-section';
import { SettingsNav } from './settings-nav';
import { TimezoneSelect } from './timezone-select';

interface SettingsPageProps {
  sessions: Session[];
  invites: Invite[];
  timezone: string;
  canCreateInvites?: boolean;
  canManageWorkspace?: boolean;
  workspaceTimezone?: string;
  workspaceName?: string;
  workspaceNameSave?: SaveState;
  onSetWorkspaceName?: (name: string) => void;
  timezoneOverride?: string | null;
  // email and displayName are the person signed in; an empty name shows the email.
  email?: string;
  displayName?: string;
  onSetDisplayName?: (name: string) => void;
  displayNameSave?: SaveState;
  createdInviteUrl?: string;
  onCreateInvite?: (email: string, role: 'admin' | 'agent') => void;
  onRevokeSession?: (id: string) => void;
  onRevokeInvite?: (id: string) => void;
  onSetWorkspaceTimezone?: (timezone: string) => void;
  onSetAgentTimezone?: (timezone: string | null) => void;
  loading?: boolean;
  error?: string;
  workspaceTimezoneSave?: SaveState;
  personalTimezoneSave?: SaveState;
  // inviteError explains a refused invite, for example a full agent cap.
  inviteError?: string;
  // teamSection lists the agents; invites and sessions follow it, as they belong to the same people.
  teamSection?: ReactNode;
  emailSection?: ReactNode;
  // savedRepliesSection is for every agent, not only admins.
  savedRepliesSection?: ReactNode;
  // exportSection is the admin's whole-workspace export.
  exportSection?: ReactNode;
  webFormSection?: ReactNode;
  // helpCenterSection turns the public help center on or off; every agent sees it, admins change it.
  helpCenterSection?: ReactNode;
  // showBilling adds Plan and billing to the nav; only admins of a billed (cloud) workspace see it.
  showBilling?: boolean;
}

// SettingsPage is one page in two groups: what belongs to the person signed in, then what the whole
// workspace shares. A section nav jumps to each; it lists only the sections this person can see,
// plus Plan and billing (its own page) for admins of a billed workspace.
export function SettingsPage({ sessions, invites, timezone, canCreateInvites = false, canManageWorkspace = false,
  workspaceTimezone = 'UTC', workspaceName = '', workspaceNameSave, onSetWorkspaceName, timezoneOverride = null, email, displayName = '', onSetDisplayName, displayNameSave, createdInviteUrl, onCreateInvite, onRevokeInvite,
  onRevokeSession, onSetWorkspaceTimezone, onSetAgentTimezone, loading = false, error, workspaceTimezoneSave, personalTimezoneSave, inviteError, teamSection, emailSection, savedRepliesSection, webFormSection, helpCenterSection, exportSection, showBilling = false }: SettingsPageProps) {
  return (
    <main className="settings-page panel">
      <header><h1>{messages.settings}</h1><p>{messages.settingsDescription}</p></header>
      {loading ? <p role="status" className="empty-copy">{messages.loading}</p> : <div className="settings-layout">
        <SettingsNav page="/settings" shown={{ general: canManageWorkspace, team: Boolean(teamSection), email: Boolean(emailSection), webForm: Boolean(webFormSection),
          savedReplies: Boolean(savedRepliesSection), helpCenter: Boolean(helpCenterSection), exportData: Boolean(exportSection), billing: showBilling, importData: canManageWorkspace }} />
        <div className="settings-sections">
          {error && <p role="alert" className="error-copy">{error}</p>}
          <p className="settings-group-label">{messages.settingsYourAccount}</p>
          <section className="settings-card" aria-labelledby="profile-heading">
            <div className="section-heading"><h2 id="profile-heading">{messages.profile}</h2><p>{messages.profileDescription}</p></div>
            <div className="settings-body">
              <DisplayNameField value={displayName} email={email} save={displayNameSave} onSave={onSetDisplayName} />
              <TimezoneSelect label={messages.yourTimezone} value={timezoneOverride ?? ''} emptyOption={messages.sameAsWorkspace(workspaceTimezone)}
                save={personalTimezoneSave} onChange={(value) => onSetAgentTimezone?.(value || null)} />
            </div>
          </section>
          <PreferencesSection />
          <p className="settings-group-label">{messages.settingsWorkspace}</p>
          {canManageWorkspace && <section className="settings-card" aria-labelledby="general-heading">
            <div className="section-heading"><h2 id="general-heading">{messages.general}</h2><p>{messages.generalDescription}</p></div>
            <div className="settings-body">
              <NameForm label={messages.workspaceName} saveLabel={messages.saveWorkspaceName} hint={messages.workspaceNameHelp}
                value={workspaceName} canManage save={workspaceNameSave} onSave={onSetWorkspaceName} minLength={2} maxLength={60} />
              <TimezoneSelect label={messages.workspaceTimezone} value={workspaceTimezone} save={workspaceTimezoneSave} onChange={(value) => onSetWorkspaceTimezone?.(value)} />
            </div>
          </section>}
          {teamSection}
          <InvitesSection invites={invites} timezone={timezone} canCreateInvites={canCreateInvites} createdInviteUrl={createdInviteUrl}
            inviteError={inviteError} onCreateInvite={onCreateInvite} onRevokeInvite={onRevokeInvite} />
          <SessionsSection sessions={sessions} timezone={timezone} onRevokeSession={onRevokeSession} />
          {emailSection}
          {webFormSection}
          {savedRepliesSection}
          {helpCenterSection}
          {exportSection && <section className="settings-card" aria-labelledby="export-heading">
            <div className="section-heading"><h2 id="export-heading">{messages.exportHeading}</h2><p>{messages.exportDescription}</p></div>
            <div className="settings-body">{exportSection}</div>
          </section>}
        </div>
      </div>}
    </main>
  );
}
