import type { ReactNode } from 'react';
import type { Invite, Session } from '../../api/schemas';
import { messages } from '../../messages';
import { InvitesSection } from './invites-section';
import { PreferencesSection } from './preferences-section';
import type { SaveState } from './save-feedback';
import { SessionsSection } from './sessions-section';
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
}

interface NavEntry { label: string; target: string; shown: boolean }

// SettingsPage is one page in two groups: what belongs to the person signed in, then what the whole
// workspace shares. A section nav jumps to each; it lists only the sections this person can see.
export function SettingsPage({ sessions, invites, timezone, canCreateInvites = false, canManageWorkspace = false,
  workspaceTimezone = 'UTC', timezoneOverride = null, createdInviteUrl, onCreateInvite, onRevokeInvite,
  onRevokeSession, onSetWorkspaceTimezone, onSetAgentTimezone, loading = false, error, workspaceTimezoneSave, personalTimezoneSave, inviteError, teamSection, emailSection, savedRepliesSection, webFormSection, exportSection }: SettingsPageProps) {
  const groups: { label: string; entries: NavEntry[] }[] = [
    { label: messages.settingsYourAccount, entries: [
      { label: messages.profile, target: 'profile-heading', shown: true },
      { label: messages.preferences, target: 'preferences-heading', shown: true },
    ] },
    { label: messages.settingsWorkspace, entries: [
      { label: messages.general, target: 'general-heading', shown: canManageWorkspace },
      { label: messages.team, target: 'team-heading', shown: Boolean(teamSection) },
      { label: messages.invites, target: 'invites-heading', shown: true },
      { label: messages.sessions, target: 'sessions-heading', shown: true },
      { label: messages.emailChannel, target: 'email-channel-heading', shown: Boolean(emailSection) },
      { label: messages.webForm, target: 'web-form-heading', shown: Boolean(webFormSection) },
      { label: messages.savedReplies, target: 'saved-replies-heading', shown: Boolean(savedRepliesSection) },
      { label: messages.exportHeading, target: 'export-heading', shown: Boolean(exportSection) },
    ] },
  ];
  return (
    <main className="settings-page panel">
      <header><h1>{messages.settings}</h1><p>{messages.settingsDescription}</p></header>
      {loading ? <p role="status" className="empty-copy">{messages.loading}</p> : <div className="settings-layout">
        <nav className="settings-nav" aria-label={messages.settingsSections}>
          {groups.map((group) => <div key={group.label}><p className="settings-group-label">{group.label}</p>
            <ul>{group.entries.filter((entry) => entry.shown).map((entry) => <li key={entry.target}><a href={`#${entry.target}`}>{entry.label}</a></li>)}</ul>
          </div>)}
        </nav>
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
