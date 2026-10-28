import type { ReactNode } from 'react';
import { settingsMessages } from './settings-messages';

// ServerDetails folds what only the person who runs the server needs (hosts, ports, redirect URIs) away
// from the task at hand; callers show it to admins only.
export function ServerDetails({ children }: { children: ReactNode }) {
  return <details className="email-channel-dns-folded server-details"><summary>{settingsMessages.serverDetails}</summary>
    <p className="muted-copy">{settingsMessages.serverDetailsHelp}</p>{children}</details>;
}
