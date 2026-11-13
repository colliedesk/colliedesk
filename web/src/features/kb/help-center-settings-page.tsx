import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { messages } from '../../messages';
import { HelpCenterSettings } from './help-center-card';
import { kbMessages } from './kb-messages';
import { KBHead } from './kb-ui';

// Knowledge base → Settings (/knowledge/settings): whether the help center is public, its title, domain, brand
// and Search Console. It was a section of Settings; it lives beside the articles it publishes now (#528).
export function HelpCenterSettingsRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  return (
    <main className="kb-frame kb-settings-page" aria-labelledby="kb-heading">
      <KBHead current="settings" heading={kbMessages.heading} />
      <section className="settings-card" aria-labelledby="help-center-heading">
        <div className="section-heading"><h2 id="help-center-heading">{messages.helpCenter}</h2><p>{kbMessages.helpCenterDescription}</p></div>
        <div className="settings-body">{me.data ? <HelpCenterSettings canManage={can(me.data, 'help_center:update')} workspaceName={me.data.workspace.name} timezone={me.data.timezone ?? 'UTC'} />
          : <p role="status" className="muted-copy">{messages.loading}</p>}</div>
      </section>
    </main>
  );
}
