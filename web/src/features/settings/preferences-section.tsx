import { useEffect, useState } from 'react';
import { applyTheme, onThemeChange, saveTheme, storedTheme } from '../../lib/theme';
import type { Theme } from '../../lib/theme';
import { messages } from '../../messages';
import { NotificationsSetting } from './notifications-setting';

// Preferences are per-browser choices: saved locally, not on the agent profile.
export function PreferencesSection() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  // Another tab saved a different theme: follow it, and show it as the choice.
  useEffect(() => onThemeChange((next) => { setTheme(next); applyTheme(next); }), []);
  const choose = (value: Theme) => { setTheme(value); saveTheme(value); };
  return (
    <section className="settings-card" aria-labelledby="preferences-heading">
      <div className="section-heading"><h2 id="preferences-heading">{messages.preferences}</h2><p>{messages.preferencesDescription}</p></div>
      <div className="settings-body">
        <div className="field"><span className="field-label" id="theme-label">{messages.theme}</span>
          <fieldset className="segmented" aria-labelledby="theme-label">
            {(['light', 'dark'] as const).map((value) => (
              <label key={value}><input type="radio" name="theme" value={value} checked={theme === value} onChange={() => choose(value)} />
                <span>{messages.themes[value]}</span></label>
            ))}
          </fieldset>
        </div>
        <NotificationsSetting />
      </div>
    </section>
  );
}
