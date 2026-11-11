import type { ReactNode } from 'react';
import { messages } from '../../messages';
import { SettingsNav } from './settings-nav';
import type { SettingsPath, Shown } from './settings-nav';
import './settings-v2.css';

// Every page the menu can show; SettingsNav narrows it to what the signed-in person may see.
export const everyPage: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false, importData: true, ai: true,
  agents: true, conversations: true, invites: true, roles: true };

interface SettingsFrameProps {
  page: SettingsPath;
  title: string;
  // lead is the one line under the title saying what the page holds.
  lead?: string;
  shown?: Shown;
  loading?: boolean;
  className?: string;
  children?: ReactNode;
}

// SettingsFrame is one settings page: its serif title and the line under it, the settings menu, and the
// page's own sections beside it. While the page's first data is on its way it says so, without the forms.
export function SettingsFrame({ page, title, lead, shown = everyPage, loading = false, className, children }: SettingsFrameProps) {
  return (
    <main className={`settings-page panel settings-v2${className ? ` ${className}` : ''}`}>
      <header><h1>{title}</h1>{lead && <p>{lead}</p>}</header>
      {loading ? <p role="status" className="empty-copy">{messages.loading}</p> : <div className={page === '/settings' ? 'settings-layout settings-index' : 'settings-layout'}>
        <SettingsNav page={page} shown={shown} />
        <div className="settings-sections">{children}</div>
      </div>}
    </main>
  );
}
