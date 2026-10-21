import { ClerkProvider, SignIn, useAuth, useClerk } from '@clerk/clerk-react';
import { useEffect, useRef, useState } from 'react';
import { NotAnAgentError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import { messages } from '../../messages';
import { CloudSignup } from './cloud-signup';

interface CloudSignInProps {
  publishableKey: string;
  // onSessionToken rejects with NotAnAgentError when the verified email belongs to no workspace.
  onSessionToken: (token: string) => Promise<void>;
  // onSignUp creates a trial workspace; present only where the server offers sign-up.
  onSignUp?: (token: string, workspaceName: string, answers: SignupAnswers) => Promise<void>;
  // buying is true when the visitor came to buy a plan; sign-up then leads to payment.
  buying?: boolean;
}

// Clerk draws its own form; it reads the brand tokens of the active theme so it matches the app.
function clerkAppearance() {
  const token = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || undefined;
  return {
    variables: {
      colorBackground: token('--surface-1'), colorText: token('--text-1'), colorTextSecondary: token('--text-2'),
      colorPrimary: token('--accent'), colorTextOnPrimaryBackground: token('--on-accent'), colorInputBackground: token('--surface-0'),
      colorInputText: token('--text-1'), colorNeutral: token('--text-1'), colorDanger: token('--danger'),
      fontFamily: token('--font-sans'), borderRadius: token('--radius'),
    },
  };
}

function CloudSession({ onSessionToken, onSignUp, buying }: Pick<CloudSignInProps, 'onSessionToken' | 'onSignUp' | 'buying'>) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const clerk = useClerk();
  const exchanged = useRef(false);
  const [failed, setFailed] = useState(false);
  const [noWorkspace, setNoWorkspace] = useState(false);
  const [signOutFailed, setSignOutFailed] = useState(false);

  useEffect(() => {
    if (!isLoaded || !isSignedIn || exchanged.current || failed || noWorkspace) return;
    exchanged.current = true;
    void getToken().then((token) => {
      if (!token) throw new Error('Clerk did not return a session token');
      return onSessionToken(token);
    }).catch((error: unknown) => {
      exchanged.current = false;
      if (error instanceof NotAnAgentError) setNoWorkspace(true);
      else setFailed(true);
    });
  }, [failed, getToken, isLoaded, isSignedIn, noWorkspace, onSessionToken]);

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
    <p role="alert" className="error-copy">{messages.noWorkspaceForEmail}</p>
    <button type="button" onClick={leave}>{messages.useAnotherAccount}</button>
    {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
  </div>;
  if (failed) return <button type="button" onClick={() => setFailed(false)}>{messages.retryCloudSignIn}</button>;
  if (!isLoaded || isSignedIn) return <p>{messages.cloudSigningIn}</p>;
  // Stay on this page after sign-in (Clerk would otherwise go to "/" and unmount this component
  // before the exchange). withSignUp lets an agent who has no Clerk identity yet sign in with a
  // code; the server still admits only emails that belong to an agent.
  const here = `${window.location.pathname}${window.location.search}`;
  return <SignIn routing="hash" withSignUp forceRedirectUrl={here} signUpForceRedirectUrl={here} appearance={clerkAppearance()} />;
}

export function CloudSignIn({ publishableKey, onSessionToken, onSignUp, buying }: CloudSignInProps) {
  return <ClerkProvider publishableKey={publishableKey}>
    <CloudSession onSessionToken={onSessionToken} onSignUp={onSignUp} buying={buying} />
  </ClerkProvider>;
}
