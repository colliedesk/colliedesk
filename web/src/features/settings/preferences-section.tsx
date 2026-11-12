import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { applyTheme, onThemeChange, saveTheme, storedTheme } from '../../lib/theme';
import type { Theme } from '../../lib/theme';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { NotificationsSetting } from './notifications-setting';

// Preferences are per-browser choices, saved locally, except the mention email, which the server keeps for the person.
// mentionEmail is the mention email setting, which reads and saves on the server (MentionEmailSetting).
export function PreferencesSection({ setupGuideHidden = false, onSetSetupGuideHidden, mentionEmail }: { setupGuideHidden?: boolean; onSetSetupGuideHidden?: (hidden: boolean) => void; mentionEmail?: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  // Another tab saved a different theme: follow it, and show it as the choice.
  useEffect(() => onThemeChange((next) => { setTheme(next); applyTheme(next); }), []);
  const choose = (value: Theme) => { setTheme(value); saveTheme(value); };
  return (
    <section className="settings-card" aria-labelledby="preferences-heading">
      <div className="section-heading"><h2 id="preferences-heading">{messages.preferences}</h2><p>{settingsMessages.preferencesDescription}</p></div>
      <div className="settings-body settings-rows">
        <div className="field"><span className="field-label" id="theme-label">{settingsMessages.theme}</span>
          <fieldset className="segmented" aria-labelledby="theme-label">
            {(['light', 'dark'] as const).map((value) => (
              <label key={value}><input type="radio" name="theme" value={value} checked={theme === value} onChange={() => choose(value)} />
                <span>{settingsMessages.themes[value]}</span></label>
            ))}
          </fieldset>
        </div>
        <NotificationsSetting />
        {mentionEmail}
        {onSetSetupGuideHidden && <div className="field">
          <label className="field-inline"><input type="checkbox" checked={!setupGuideHidden} onChange={(event) => onSetSetupGuideHidden(!event.target.checked)} />
            <span>{settingsMessages.showSetupGuide}</span></label>
        </div>}
      </div>
    </section>
  );
}
