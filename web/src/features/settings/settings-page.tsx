import type { ReactNode } from 'react';
import type { Invite, Session } from '../../api/schemas';
import { messages } from '../../messages';
import { InvitesSection } from './invites-section';
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
  timezoneOverride?: string | null;
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
  // showBilling adds Plan and billing to the nav; only admins of a billed (cloud) workspace see it.
  showBilling?: boolean;
}

// SettingsPage is one page in two groups: what belongs to the person signed in, then what the whole
// workspace shares. A section nav jumps to each; it lists only the sections this person can see,
// plus Plan and billing (its own page) for admins of a billed workspace.
export function SettingsPage({ sessions, invites, timezone, canCreateInvites = false, canManageWorkspace = false,
  workspaceTimezone = 'UTC', timezoneOverride = null, createdInviteUrl, onCreateInvite, onRevokeInvite,
  onRevokeSession, onSetWorkspaceTimezone, onSetAgentTimezone, loading = false, error, workspaceTimezoneSave, personalTimezoneSave, inviteError, teamSection, emailSection, savedRepliesSection, webFormSection, exportSection, showBilling = false }: SettingsPageProps) {
  return (
    <main className="settings-page panel">
      <header><h1>{messages.settings}</h1><p>{messages.settingsDescription}</p></header>
      {loading ? <p role="status" className="empty-copy">{messages.loading}</p> : <div className="settings-layout">
        <SettingsNav page="/settings" shown={{ general: canManageWorkspace, team: Boolean(teamSection), email: Boolean(emailSection), webForm: Boolean(webFormSection),
          savedReplies: Boolean(savedRepliesSection), exportData: Boolean(exportSection), billing: showBilling }} />
        <div className="settings-sections">
          {error && <p role="alert" className="error-copy">{error}</p>}
          <p className="settings-group-label">{messages.settingsYourAccount}</p>
          <section className="settings-card" aria-labelledby="profile-heading">
            <div className="section-heading"><h2 id="profile-heading">{messages.profile}</h2><p>{messages.profileDescription}</p></div>
            <div className="settings-body">
              <TimezoneSelect label={messages.yourTimezone} value={timezoneOverride ?? ''} emptyOption={messages.sameAsWorkspace(workspaceTimezone)}
                save={personalTimezoneSave} onChange={(value) => onSetAgentTimezone?.(value || null)} />
            </div>
          </section>
          <PreferencesSection />
          <p className="settings-group-label">{messages.settingsWorkspace}</p>
          {canManageWorkspace && <section className="settings-card" aria-labelledby="general-heading">
            <div className="section-heading"><h2 id="general-heading">{messages.general}</h2><p>{messages.generalDescription}</p></div>
            <div className="settings-body">
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
          {exportSection && <section className="settings-card" aria-labelledby="export-heading">
            <div className="section-heading"><h2 id="export-heading">{messages.exportHeading}</h2><p>{messages.exportDescription}</p></div>
            <div className="settings-body">{exportSection}</div>
          </section>}
        </div>
      </div>}
    </main>
  );
}
