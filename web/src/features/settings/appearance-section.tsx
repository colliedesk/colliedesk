import { useState } from 'react';
import { saveTheme, storedTheme } from '../../lib/theme';
import type { Theme } from '../../lib/theme';
import { messages } from '../../messages';

// Appearance is a per-browser choice: it is saved locally, not on the agent profile.
export function AppearanceSection() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  const choose = (value: Theme) => { setTheme(value); saveTheme(value); };
  return (
    <section className="settings-card" aria-labelledby="appearance-heading">
      <div className="section-heading"><h2 id="appearance-heading">{messages.appearance}</h2><p>{messages.appearanceDescription}</p></div>
      <div className="settings-body">
        <fieldset className="segmented" aria-label={messages.theme}>
          {(['light', 'dark'] as const).map((value) => (
            <label key={value}><input type="radio" name="theme" value={value} checked={theme === value} onChange={() => choose(value)} />
              <span>{messages.themes[value]}</span></label>
          ))}
        </fieldset>
      </div>
    </section>
  );
}
