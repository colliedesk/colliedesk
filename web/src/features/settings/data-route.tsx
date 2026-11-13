import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { useScrollToHashWhenReady } from '../../lib/scroll-to-hash';
import { messages } from '../../messages';
import { useBillingShown } from '../billing/billing-shown';
import './data-export.css';
import { ExportAllData } from './export-all-data';
import { importMessages } from './import-messages';
import { ImportRoute } from './import-route';
import { everyPage, SettingsFrame } from './settings-frame';
import { settingsMessages } from './settings-messages';

// Settings → Data: bring the Freshdesk history in (imports:run) or take every conversation out (workspace:export).
// Its own chunk, with the import's steps; a person who may do neither is asked for nothing but who they are.
export function DataRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  const imports = can(me.data, 'imports:run');
  const exports = can(me.data, 'workspace:export');
  useScrollToHashWhenReady(!me.isPending);
  return <SettingsFrame page="/settings/data" title={messages.dataPage} lead={settingsMessages.pageLeads.data} loading={me.isPending}
    shown={{ ...everyPage, billing: billingShown }} className="import-page data-page">
    {imports && <>
      <p className="settings-group-label">{importMessages.heading}</p>
      <ImportRoute />
    </>}
    {exports && <section className="settings-card data-export" aria-labelledby="export-heading">
      <div className="section-heading"><h2 id="export-heading">{messages.exportHeading}</h2><p>{messages.exportDescription}</p></div>
      <div className="settings-body settings-rows"><ExportAllData /></div>
    </section>}
    {/* The import's email links here: someone who may do neither is told who can, as the import's page told them. */}
    {!imports && !exports && <div className="settings-card"><p className="muted-copy">{importMessages.adminsOnly}</p>
      <p className="muted-copy">{settingsMessages.exportForbidden}</p></div>}
  </SettingsFrame>;
}
