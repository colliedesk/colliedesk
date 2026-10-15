import type { ReactNode } from 'react';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { LogoMark } from '../../ui/logo';

interface RedeemPageProps {
  token: string;
  pending?: boolean;
  failed?: boolean;
  onRedeem?: () => void;
  cloudEnabled?: boolean;
  cloudSignIn?: ReactNode;
  workspaces?: WorkspaceChoice[];
  onSelectWorkspace?: (slug: string) => void;
}

export function RedeemPage({
  token, pending, failed, onRedeem, cloudEnabled = false, cloudSignIn, workspaces = [], onSelectWorkspace,
}: RedeemPageProps) {
  return (
    <main className="redeem-page signin-split">
      <div className="signin-brand">
        <div className="signin-brand-lockup"><LogoMark size={40} /><span className="wordmark"><b>collie</b>desk</span></div>
        <p className="signin-tagline">{messages.signInTagline}</p>
        <p className="signin-footnote">{messages.signInFootnote}</p>
      </div>
      <section className="redeem-panel">
        <div className="brand-lockup"><LogoMark size={36} /><span>{messages.brand}</span></div>
        <h1>{messages.openSecureLink}</h1>
        <p>{messages.redeemExplanation}</p>
        {!token && !cloudEnabled && <p className="muted-copy">{messages.requestLink}</p>}
        {!token && cloudEnabled && <div className="cloud-sign-in">
          {workspaces.length > 0 ? <>
            <h2>{messages.chooseWorkspace}</h2>
            <div className="workspace-choices">
              {workspaces.map((workspace) => <button type="button" key={workspace.slug}
                onClick={() => onSelectWorkspace?.(workspace.slug)} disabled={pending}>{workspace.name}</button>)}
            </div>
          </> : cloudSignIn}
          <p className="muted-copy">{messages.oneTimeLinkInstead}</p>
        </div>}
        {failed && workspaces.length === 0 && <p role="alert" className="error-copy">
          {token ? messages.invalidLink : messages.cloudSignInFailed}
        </p>}
        {token && <button className="primary-action" type="button" onClick={onRedeem} disabled={pending}>
          {pending ? messages.redeeming : messages.continue}
        </button>}
      </section>
    </main>
  );
}
