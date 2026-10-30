import { billingMessages } from './billing-messages';

export type MigrationPlan = 'founding' | 'team25';

// A mail link breaks lines with CRLF (RFC 6068).
const mailLink = (workspaceName: string) =>
  `mailto:${billingMessages.supportEmail}?subject=${encodeURIComponent(billingMessages.migrationSubject(workspaceName))}`
  + `&body=${encodeURIComponent(billingMessages.migrationBody.replace(/\n/g, '\r\n'))}`;

// MigrationCard is the next step after paying for Founding or Team 25: send us the Freshdesk address, or
// run the import from the app.
export function MigrationCard({ plan, workspaceName }: { plan: MigrationPlan; workspaceName: string }) {
  return (
    <section className="migration-card" aria-labelledby="migration-heading">
      <h2 id="migration-heading">{billingMessages.migrationHeading}</h2>
      <p>{plan === 'founding' ? billingMessages.migrationFounding : billingMessages.migrationTeam25}</p>
      <div className="button-row">
        <a className="button" href={mailLink(workspaceName)}>{billingMessages.migrationEmail}</a>
        <a href="/settings/import-freshdesk">{billingMessages.migrationSelfServe}</a>
      </div>
    </section>
  );
}
