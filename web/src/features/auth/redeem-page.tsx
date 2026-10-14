import type { ReactNode } from 'react';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';

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
    <main className="redeem-page">
      <section className="redeem-panel">
        <div className="brand-mark" aria-hidden="true">{messages.brandInitial}</div>
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
