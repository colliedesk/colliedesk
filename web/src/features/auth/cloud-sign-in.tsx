import { ClerkProvider, SignIn, useAuth } from '@clerk/clerk-react';
import { useEffect, useRef, useState } from 'react';
import { messages } from '../../messages';

interface CloudSignInProps {
  publishableKey: string;
  onSessionToken: (token: string) => Promise<void>;
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

function CloudSession({ onSessionToken }: Pick<CloudSignInProps, 'onSessionToken'>) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const exchanged = useRef(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
	if (!isLoaded || !isSignedIn || exchanged.current || failed) return;
    exchanged.current = true;
    void getToken().then((token) => {
      if (!token) throw new Error('Clerk did not return a session token');
		return onSessionToken(token);
	}).catch(() => {
		exchanged.current = false;
		setFailed(true);
	});
  }, [failed, getToken, isLoaded, isSignedIn, onSessionToken]);

  if (failed) return <button type="button" onClick={() => setFailed(false)}>{messages.retryCloudSignIn}</button>;
  if (!isLoaded || isSignedIn) return <p>{messages.cloudSigningIn}</p>;
  // Stay on this page after sign-in (Clerk would otherwise go to "/" and unmount this component
  // before the exchange). withSignUp lets an agent who has no Clerk identity yet sign in with a
  // code; the server still admits only emails that belong to an agent.
  const here = `${window.location.pathname}${window.location.search}`;
  return <SignIn routing="hash" withSignUp forceRedirectUrl={here} signUpForceRedirectUrl={here} appearance={clerkAppearance()} />;
}

export function CloudSignIn({ publishableKey, onSessionToken }: CloudSignInProps) {
  return <ClerkProvider publishableKey={publishableKey}>
    <CloudSession onSessionToken={onSessionToken} />
  </ClerkProvider>;
}
