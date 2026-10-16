import { messages } from '../../messages';
import { disableNotifications, enableNotifications, useNotificationState } from '../shell/notification-preference';

// NotificationsSetting is one checkbox; the choice belongs to this browser. The browser permission
// is asked only when the agent ticks it.
export function NotificationsSetting() {
  const state = useNotificationState();
  if (state === 'unsupported') return null;
  return (
    <div className="field">
      <label className="field-inline"><input type="checkbox" checked={state === 'on'} disabled={state === 'blocked'}
        onChange={(event) => (event.target.checked ? void enableNotifications() : disableNotifications())} />
        <span>{messages.notificationsLabel}</span></label>
      {state === 'blocked' && <small className="muted-copy">{messages.notificationsBlocked}</small>}
    </div>
  );
}
