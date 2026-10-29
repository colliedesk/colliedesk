import { ClerkProvider } from '@clerk/clerk-react';
import { useCallback, useEffect } from 'react';
import { preconnectClerk } from '../features/auth/clerk-appearance';
import { ClerkSession } from '../features/auth/clerk-session';
import type { RefusedIdentity } from '../features/auth/clerk-session';
import { ApiError, api } from './api';
import type { SignInStep } from './api';
import { CommandLine } from './copy';
import { cloudMessages as c } from './cloud-messages';
import { m } from './messages';

// The console's cloud sign-in, a lazy part: the identity provider's form (the workspace app's ClerkSession) and
// the hand-over of its token to POST /api/v1/operator/auth/cloud. Only the sign-in page loads it, under the
// page's own content policy (server/internal/webui signInCSP).
const copy = { signingOut: c.signingOut, signOutFailed: c.signOutFailed, tryAgain: m.tryAgain, signingIn: m.continuing, retry: c.retry };

const isRefused = (error: unknown) => error instanceof ApiError && error.code === 'operator_sign_in_refused';

function Refused({ leave, signOutFailed }: RefusedIdentity) {
  return <>
    <p role="alert" className="error-copy">{c.refused}</p>
    <p>{c.refusedHelp}</p>
    <CommandLine command="docker compose exec server /app/collie operator add --email <their email>" />
    <button type="button" onClick={() => void leave()}>{c.useAnotherAccount}</button>
    {signOutFailed && <p role="alert" className="error-copy">{c.signOutFailed}</p>}
  </>;
}

export default function CloudSignIn({ publishableKey, endSession, onStep, onError }: {
  publishableKey: string;
  endSession: boolean;
  onStep: (step: SignInStep) => void;
  // onError takes what the sign-in page explains itself (a lock, a server without its key ring).
  onError: (error: ApiError) => void;
}) {
  useEffect(() => { preconnectClerk(publishableKey); }, [publishableKey]);
  const exchange = useCallback(async (token: string) => {
    try {
      onStep(await api.cloudSignIn(token));
    } catch (error) {
      if (error instanceof ApiError && (error.code === 'operator_locked' || error.code === 'secret_keys_missing')) onError(error);
      else throw error;
    }
  }, [onError, onStep]);
  return <ClerkProvider publishableKey={publishableKey}>
    <ClerkSession copy={copy} onSessionToken={exchange} isRefused={isRefused} refused={(identity) => <Refused {...identity} />} endSession={endSession} />
  </ClerkProvider>;
}
