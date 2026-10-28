import type { ReactNode } from 'react';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { LogoMark, LogoWordmark } from '../../ui/logo';

interface RedeemPageProps {
  token: string;
  pending?: boolean;
  failed?: boolean;
  // planLimited means the link is valid but the workspace's plan has no free agent seat.
  planLimited?: boolean;
  onRedeem?: () => void;
  cloudEnabled?: boolean;
  // signupEnabled adds the line that tells a new customer the same sign-in creates a workspace.
  signupEnabled?: boolean;
  cloudSignIn?: ReactNode;
  workspaces?: WorkspaceChoice[];
  onSelectWorkspace?: (slug: string) => void;
  // buying is the plan a visitor came from the website to buy; the page then greets a buyer.
  buying?: string;
  // signedOut is true when the browser arrived here because its session ended.
  signedOut?: boolean;
  // trial is true for the website's "Start free trial" link; the page then greets a new team.
  trial?: boolean;
}

export function RedeemPage({
  token, pending, failed, planLimited = false, onRedeem, cloudEnabled = false, signupEnabled = false, cloudSignIn, workspaces = [], onSelectWorkspace, buying, signedOut = false, trial = false,
}: RedeemPageProps) {
  const buyer = !token && cloudEnabled && buying !== undefined;
  // A new team can only start a trial where sign-up is on; a buy link wins, since the buyer pays at once.
  const newTeam = !token && cloudEnabled && signupEnabled && trial && !buyer;
  return (
    <main className="redeem-page signin-split">
      <div className="signin-brand">
        <div className="signin-brand-lockup"><LogoMark size={40} /><LogoWordmark /></div>
        <p className="signin-tagline">{messages.signInTagline}</p>
        <p className="signin-status">{messages.signInAlpha}</p>
        <p className="signin-footnote">{messages.signInFootnote}</p>
      </div>
      <section className="redeem-panel">
        <div className="brand-lockup"><LogoMark size={36} /><span>{messages.brand}</span></div>
        {signedOut && <p role="alert" className="muted-copy">{messages.signedOutCloud}</p>}
        {/* Without a link in hand, cloud sign-in is an emailed code, so the page is titled as a sign-in. */}
        <h1>{buyer ? messages.buyingTitle(buying) : newTeam ? messages.cloudTrialTitle : !token && cloudEnabled ? messages.cloudSignInTitle : messages.openSecureLink}</h1>
        <p>{buyer ? messages.buyingExplanation : newTeam ? messages.cloudTrialExplanation : !token && cloudEnabled ? messages.cloudSignInExplanation : messages.redeemExplanation}</p>
        {!token && !cloudEnabled && <p className="muted-copy">{messages.requestLink}</p>}
        {!token && cloudEnabled && <div className="cloud-sign-in">
          {workspaces.length > 0 ? <>
            <h2>{messages.chooseWorkspace}</h2>
            <div className="workspace-choices">
              {workspaces.map((workspace) => <button type="button" key={workspace.slug}
                onClick={() => onSelectWorkspace?.(workspace.slug)} disabled={pending}>{workspace.name}</button>)}
            </div>
          </> : cloudSignIn}
          {newTeam && workspaces.length === 0 && <p className="muted-copy">{messages.cloudTrialWorkspace}</p>}
          {newTeam && workspaces.length === 0 && <p className="muted-copy">{messages.cloudTrialSignInInstead}</p>}
          {!buyer && !newTeam && signupEnabled && workspaces.length === 0 && <p className="muted-copy">{messages.cloudNewHere}</p>}
          {!buyer && !newTeam && <p className="muted-copy">{messages.oneTimeLinkInstead}</p>}
          {/* Where a visitor can start a workspace, the page says what they agree to and links the prices. */}
          {signupEnabled && <p className="muted-copy signin-consent">{messages.consentBefore}
            <a href={messages.termsURL} target="_blank" rel="noreferrer">{messages.consentTerms}</a>{messages.consentAnd}
            <a href={messages.privacyURL} target="_blank" rel="noreferrer">{messages.consentPrivacy}</a>.{' '}{messages.consentSee}
            <a href={messages.pricingURL} target="_blank" rel="noreferrer">{messages.consentPricing}</a>.</p>}
        </div>}
        {failed && workspaces.length === 0 && <p role="alert" className="error-copy">
          {token ? (planLimited ? messages.linkPlanLimited : messages.invalidLink) : messages.cloudSignInFailed}
        </p>}
        {token && <button className="primary-action" type="button" onClick={onRedeem} disabled={pending}>
          {pending ? messages.redeeming : messages.continue}
        </button>}
      </section>
    </main>
  );
}
