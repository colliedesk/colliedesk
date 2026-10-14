import { ClerkProvider, SignIn, useAuth } from '@clerk/clerk-react';
import { useEffect, useRef, useState } from 'react';
import { messages } from '../../messages';

interface CloudSignInProps {
  publishableKey: string;
  onSessionToken: (token: string) => Promise<void>;
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
  return <SignIn routing="hash" />;
}

export function CloudSignIn({ publishableKey, onSessionToken }: CloudSignInProps) {
  return <ClerkProvider publishableKey={publishableKey}>
    <CloudSession onSessionToken={onSessionToken} />
  </ClerkProvider>;
}
