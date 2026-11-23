import { useId } from 'react';
import type { PortalHelpCenter } from '../../api/imports';
import { importMessages as m } from './import-messages';

function outcome(portal: PortalHelpCenter): string {
  const name = portal.help_center_name ?? '';
  switch (portal.status) {
    case 'primary': return m.portalPrimary(name);
    case 'created': return m.portalCreated(name, portal.handle ?? '');
    case 'existing': return m.portalExisting(name, portal.handle ?? '');
    case 'planned': return m.portalPlanned;
    case 'over_limit': return m.portalOverLimit;
    case 'trashed': return m.portalTrashed;
    default: return m.portalFailed;
  }
}

// ImportHelpCenters says which help center each Freshdesk portal becomes (in the preview) or became
// (in the report). An account with one portal has everything on the primary help center, as before,
// so it shows nothing.
export function ImportHelpCenters({ portals }: { portals: PortalHelpCenter[] | null | undefined }) {
  const heading = useId();
  if (!portals || portals.length < 2) return null;
  const unnamed = portals.filter((portal) => !portal.name_from_freshdesk).map((portal) => portal.portal_id);
  return <section className="import-help-centers" aria-labelledby={heading}>
    <h3 id={heading} className="settings-group-label">{m.portalsHeading}</h3>
    <ul>{portals.map((portal) => <li key={portal.portal_id}>{m.portalLine(portal.portal_name, portal.portal_id, portal.categories, outcome(portal))}</li>)}</ul>
    {unnamed.length > 0 && <p>{m.portalsUnnamed(unnamed)}</p>}
    {/* A retried run reports the help center its first attempt made as existing, still off. */}
    {portals.some((portal) => portal.status === 'created' || portal.status === 'existing') && <p>{m.portalsStartOff}</p>}
  </section>;
}
