import { messages } from '../../messages';
import { BellIcon } from '../../ui/icons';
import { disableNotifications, enableNotifications, useNotificationState } from './notification-preference';

// NotificationToggle is the only place that asks the browser for notification permission, and only on a click.
export function NotificationToggle() {
  const state = useNotificationState();
  if (state === 'unsupported') return null;
  const label = { blocked: messages.notificationsBlocked, off: messages.notificationsEnable, on: messages.notificationsOn }[state];
  return <button type="button" className="rail-link" data-state={state} data-tip={label} aria-label={label} disabled={state === 'blocked'}
    onClick={() => (state === 'on' ? disableNotifications() : void enableNotifications())}><BellIcon /></button>;
}
