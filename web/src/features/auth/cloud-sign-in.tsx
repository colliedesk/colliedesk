import { ClerkProvider, SignIn, useAuth, useClerk } from '@clerk/clerk-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { NotAnAgentError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import { markSignInFormReady } from '../../lib/sign-in-ready';
import { messages } from '../../messages';
import { signupMessages } from './signup-messages';
import { clerkAppearance } from './clerk-appearance';
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
  // endSession is true right after the Colliedesk session ended: an identity session still open in
  // this browser is signed out before anything is exchanged, or the same person would be signed
  // straight back in without a code.
  endSession?: boolean;
}

// The page Clerk returns to after sign-in: this one, without the signed-out marker, so the reload
// after a completed sign-in never ends the new session.
function returnAddress() {
  const search = new URLSearchParams(window.location.search);
  search.delete('signed_out');
  const query = search.toString();
  return `${window.location.pathname}${query ? `?${query}` : ''}`;
}

function CloudSession({ onSessionToken, onSignUp, buying, findWorkspaceAt, endSession = false }: Omit<CloudSignInProps, 'publishableKey'>) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const clerk = useClerk();
  const exchanged = useRef(false);
  const [failed, setFailed] = useState(false);
  const [noWorkspace, setNoWorkspace] = useState(false);
  const [signOutFailed, setSignOutFailed] = useState(false);
  // Checked once, when Clerk has loaded: a sign-in completed on this page afterwards is kept.
  const [ending, setEnding] = useState(endSession);
  const ended = useRef(false);
  // Once Clerk's form shows (its UI loads after isLoaded), what the page held back (product analytics)
  // may download.
  useEffect(() => {
    if (!isLoaded) return undefined;
    const shown = () => document.querySelector('.cl-formButtonPrimary, .cl-socialButtonsBlockButton, .cl-formFieldInput') !== null;
    if (shown()) { markSignInFormReady(); return undefined; }
    const watch = new MutationObserver(() => { if (shown()) { watch.disconnect(); markSignInFormReady(); } });
    watch.observe(document.body, { childList: true, subtree: true });
    return () => watch.disconnect();
  }, [isLoaded]);

  const endOpenSession = useCallback(async () => {
    try {
      await clerk.signOut();
      setSignOutFailed(false);
      setEnding(false);
    } catch {
      setSignOutFailed(true);
    }
  }, [clerk]);
  useEffect(() => {
    if (!ending || !isLoaded || ended.current) return;
    ended.current = true;
    if (isSignedIn) void endOpenSession();
    else setEnding(false);
  }, [endOpenSession, ending, isLoaded, isSignedIn]);

  useEffect(() => {
    if (ending || !isLoaded || !isSignedIn || exchanged.current || failed || noWorkspace) return;
    exchanged.current = true;
    void getToken().then((token) => {
      if (!token) throw new Error('Clerk did not return a session token');
      return onSessionToken(token);
    }).catch((error: unknown) => {
      exchanged.current = false;
      if (error instanceof NotAnAgentError) setNoWorkspace(true);
      else setFailed(true);
    });
  }, [ending, failed, getToken, isLoaded, isSignedIn, noWorkspace, onSessionToken]);

  // Leaving the sign-up form means signing the identity provider session out, or the next sign-in
  // would be the same person again.
  const leave = async () => {
    try {
      await clerk.signOut();
      setSignOutFailed(false);
      setNoWorkspace(false);
    } catch {
      setSignOutFailed(true);
    }
  };
  if (ending) return signOutFailed ? <div className="cloud-signup">
    <p role="alert" className="error-copy">{messages.signOutFailed}</p>
    <button type="button" onClick={() => void endOpenSession()}>{messages.tryAgain}</button>
  </div> : <p>{signupMessages.signingOut}</p>;
  if (noWorkspace && onSignUp) {
    // Clerk session tokens live for about a minute, so the token is fetched when the form is sent.
    const create = async (name: string, answers: SignupAnswers) => {
      const token = await getToken();
      if (!token) throw new Error('Clerk did not return a session token');
      await onSignUp(token, name, answers);
    };
    return <>
      <CloudSignup buying={buying} onCreate={create} onSignIn={leave} onUseAnotherAccount={leave} />
      {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
    </>;
  }
  if (noWorkspace) return <div className="cloud-signup">
    <p role="alert" className="error-copy">{findWorkspaceAt ? signupMessages.notAnAgentHere : signupMessages.noWorkspaceForEmail}</p>
    {findWorkspaceAt && <a href={findWorkspaceAt}>{signupMessages.findYourWorkspace}</a>}
    <button type="button" onClick={leave}>{signupMessages.useAnotherAccount}</button>
    {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
  </div>;
  if (failed) return <button type="button" onClick={() => setFailed(false)}>{signupMessages.retryCloudSignIn}</button>;
  if (!isLoaded || isSignedIn) return <p>{signupMessages.cloudSigningIn}</p>;
  // Stay on this page after sign-in (Clerk would otherwise go to "/" and unmount this component
  // before the exchange). withSignUp lets an agent who has no Clerk identity yet sign in with a
  // code; the server still admits only emails that belong to an agent.
  const here = returnAddress();
  return <SignIn routing="hash" withSignUp forceRedirectUrl={here} signUpForceRedirectUrl={here} appearance={clerkAppearance()} />;
}

export function CloudSignIn({ publishableKey, ...session }: CloudSignInProps) {
  return <ClerkProvider publishableKey={publishableKey}>
    <CloudSession {...session} />
  </ClerkProvider>;
}
