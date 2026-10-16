import { messages } from '../../messages';
import { disableNotifications, enableNotifications, useNotificationState } from '../shell/notification-preference';

// NotificationsSection is one checkbox; like appearance, the choice belongs to this browser.
// The browser permission is asked only when the agent ticks it.
export function NotificationsSection() {
  const state = useNotificationState();
  if (state === 'unsupported') return null;
  return (
    <section className="settings-card" aria-labelledby="notifications-heading">
      <div className="section-heading"><h2 id="notifications-heading">{messages.notificationsHeading}</h2>
        {state === 'blocked' && <p>{messages.notificationsBlocked}</p>}</div>
      <div className="settings-body">
        <label className="field-inline"><input type="checkbox" checked={state === 'on'} disabled={state === 'blocked'}
          onChange={(event) => (event.target.checked ? void enableNotifications() : disableNotifications())} />
          <span>{messages.notificationsLabel}</span></label>
      </div>
    </section>
  );
}
