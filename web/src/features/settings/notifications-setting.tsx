import { settingsMessages } from './settings-messages';
import { currentNotificationState, disableNotifications, enableNotifications, useNotificationState } from '../shell/notification-preference';
import { useSettingsSaved } from './settings-edits';

// NotificationsSetting is one checkbox; the choice belongs to this browser. The browser permission
// is asked only when the agent ticks it, and the choice is saved once the browser answered.
export function NotificationsSetting() {
  const state = useNotificationState();
  const saved = useSettingsSaved();
  if (state === 'unsupported') return null;
  const choose = async (on: boolean) => {
    if (on) await enableNotifications(); else disableNotifications();
    // A refusal at the browser's prompt changed nothing: there is nothing to say was saved.
    if ((currentNotificationState() === 'on') !== on) return;
    saved(() => (on ? disableNotifications() : enableNotifications()));
  };
  return (
    <div className="field">
      <label className="field-inline"><input type="checkbox" checked={state === 'on'} disabled={state === 'blocked'}
        onChange={(event) => void choose(event.target.checked)} />
        <span>{settingsMessages.notificationsLabel}</span></label>
      {state === 'blocked' && <small className="muted-copy" data-quiet="state">{settingsMessages.notificationsBlocked}</small>}
    </div>
  );
}
