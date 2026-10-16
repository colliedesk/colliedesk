import { useSyncExternalStore } from 'react';

const storageKey = 'colliedesk.desktop-notifications';
const changeEvent = 'colliedesk:notification-preference';

// Notification is missing in some embedded and older browsers.
export const notificationsSupported = () => typeof Notification !== 'undefined';

function read(): boolean {
  try { return localStorage.getItem(storageKey) === 'on'; } catch { return false; }
}

function write(on: boolean) {
  try {
    if (on) localStorage.setItem(storageKey, 'on'); else localStorage.removeItem(storageKey);
  } catch { /* storage blocked: the choice lasts until reload */ }
  window.dispatchEvent(new Event(changeEvent));
}

function subscribe(onChange: () => void) {
  window.addEventListener(changeEvent, onChange);
  window.addEventListener('storage', onChange);
  return () => { window.removeEventListener(changeEvent, onChange); window.removeEventListener('storage', onChange); };
}

export type NotificationState = 'unsupported' | 'blocked' | 'off' | 'on';

// currentNotificationState is the stored choice combined with the browser permission. Notifications
// are on only when the agent chose so and the browser still allows it.
export function currentNotificationState(): NotificationState {
  if (!notificationsSupported()) return 'unsupported';
  if (Notification.permission === 'denied') return 'blocked';
  return read() && Notification.permission === 'granted' ? 'on' : 'off';
}

export function useNotificationState(): NotificationState {
  return useSyncExternalStore(subscribe, currentNotificationState);
}

// enableNotifications asks the browser for permission; call it only from a click.
export async function enableNotifications(): Promise<void> {
  if (!notificationsSupported()) return;
  const permission = Notification.permission === 'default' ? await Notification.requestPermission() : Notification.permission;
  write(permission === 'granted');
}

export function disableNotifications() {
  write(false);
}
