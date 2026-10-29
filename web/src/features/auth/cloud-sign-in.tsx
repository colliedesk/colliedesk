import { ClerkProvider } from '@clerk/clerk-react';
import { NotAnAgentError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import { messages } from '../../messages';
import { signupMessages } from './signup-messages';
import { ClerkSession } from './clerk-session';
import type { RefusedIdentity } from './clerk-session';
import { CloudSignup } from './cloud-signup';

interface CloudSignInProps {
  publishableKey: string;
  // onSessionToken rejects with NotAnAgentError when the verified email belongs to no workspace.
  onSessionToken: (token: string) => Promise<void>;
  // onSignUp creates a trial workspace; present only where the server offers sign-up.
  onSignUp?: (token: string, workspaceName: string, answers: SignupAnswers) => Promise<void>;
  // buying is true when the visitor came to buy a plan; sign-up then leads to payment.
  buying?: boolean;
  // findWorkspaceAt is APP_ORIGIN's sign-in page, given on a workspace's own host: an email that is not an
  // agent of this workspace is sent there to find its own.
  findWorkspaceAt?: string;
  // endSession is true right after the Colliedesk session ended (ClerkSession).
  endSession?: boolean;
}

const copy = {
  signingOut: signupMessages.signingOut, signOutFailed: messages.signOutFailed, tryAgain: messages.tryAgain,
  signingIn: signupMessages.cloudSigningIn, retry: signupMessages.retryCloudSignIn,
};

const isNotAnAgent = (error: unknown) => error instanceof NotAnAgentError;

export function CloudSignIn({ publishableKey, onSessionToken, onSignUp, buying, findWorkspaceAt, endSession }: CloudSignInProps) {
  // A verified email of no workspace: sign up where the server offers it, else say so.
  const noWorkspace = ({ leave, getToken, signOutFailed }: RefusedIdentity) => {
    if (onSignUp) {
      const create = async (name: string, answers: SignupAnswers) => { await onSignUp(await getToken(), name, answers); };
      return <>
        <CloudSignup buying={buying} onCreate={create} onSignIn={leave} onUseAnotherAccount={leave} />
        {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
      </>;
    }
    return <div className="cloud-signup">
      <p role="alert" className="error-copy">{findWorkspaceAt ? signupMessages.notAnAgentHere : signupMessages.noWorkspaceForEmail}</p>
      {findWorkspaceAt && <a href={findWorkspaceAt}>{signupMessages.findYourWorkspace}</a>}
      <button type="button" onClick={() => void leave()}>{signupMessages.useAnotherAccount}</button>
      {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
    </div>;
  };
  return <ClerkProvider publishableKey={publishableKey}>
    <ClerkSession copy={copy} onSessionToken={onSessionToken} isRefused={isNotAnAgent} refused={noWorkspace} endSession={endSession} />
  </ClerkProvider>;
}
