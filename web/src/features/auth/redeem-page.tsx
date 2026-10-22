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
}

export function RedeemPage({
  token, pending, failed, planLimited = false, onRedeem, cloudEnabled = false, signupEnabled = false, cloudSignIn, workspaces = [], onSelectWorkspace, buying,
}: RedeemPageProps) {
  const buyer = !token && cloudEnabled && buying !== undefined;
  return (
    <main className="redeem-page signin-split">
      <div className="signin-brand">
        <div className="signin-brand-lockup"><LogoMark size={40} /><LogoWordmark /></div>
        <p className="signin-tagline">{messages.signInTagline}</p>
        <p className="signin-footnote">{messages.signInFootnote}</p>
      </div>
      <section className="redeem-panel">
        <div className="brand-lockup"><LogoMark size={36} /><span>{messages.brand}</span></div>
        {/* Without a link in hand, cloud sign-in is an emailed code, so the page is titled as a sign-in. */}
        <h1>{buyer ? messages.buyingTitle(buying) : !token && cloudEnabled ? messages.cloudSignInTitle : messages.openSecureLink}</h1>
        <p>{buyer ? messages.buyingExplanation : !token && cloudEnabled ? messages.cloudSignInExplanation : messages.redeemExplanation}</p>
        {!token && !cloudEnabled && <p className="muted-copy">{messages.requestLink}</p>}
        {!token && cloudEnabled && <div className="cloud-sign-in">
          {workspaces.length > 0 ? <>
            <h2>{messages.chooseWorkspace}</h2>
            <div className="workspace-choices">
              {workspaces.map((workspace) => <button type="button" key={workspace.slug}
                onClick={() => onSelectWorkspace?.(workspace.slug)} disabled={pending}>{workspace.name}</button>)}
            </div>
          </> : cloudSignIn}
          {!buyer && signupEnabled && workspaces.length === 0 && <p className="muted-copy">{messages.cloudNewHere}</p>}
          {!buyer && <p className="muted-copy">{messages.oneTimeLinkInstead}</p>}
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
