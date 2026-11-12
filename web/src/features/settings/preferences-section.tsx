import { useEffect, useState } from 'react';
import { applyTheme, onThemeChange, saveTheme, storedTheme } from '../../lib/theme';
import type { Theme } from '../../lib/theme';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { NotificationsSetting } from './notifications-setting';
import { MentionEmailSetting } from './mention-email-setting';
import { useHolds } from '../../lib/permissions-context';

// Preferences are per-browser choices, saved locally, except the mention email, which the server keeps for the person.
export function PreferencesSection({ setupGuideHidden = false, onSetSetupGuideHidden }: { setupGuideHidden?: boolean; onSetSetupGuideHidden?: (hidden: boolean) => void }) {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  const holds = useHolds();
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
        {holds('messages:read') && <MentionEmailSetting />}
        {onSetSetupGuideHidden && <div className="field">
          <label className="field-inline"><input type="checkbox" checked={!setupGuideHidden} onChange={(event) => onSetSetupGuideHidden(!event.target.checked)} />
            <span>{settingsMessages.showSetupGuide}</span></label>
        </div>}
      </div>
    </section>
  );
}
