import { SignIn, useAuth, useClerk } from '@clerk/clerk-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { markSignInFormReady } from '../../lib/sign-in-ready';
import { clerkAppearance } from './clerk-appearance';

// The words ClerkSession says itself; each caller passes its own, so the operator console never loads the
// workspace app's messages.
export interface ClerkSessionCopy {
  signingOut: string;
  signOutFailed: string;
  tryAgain: string;
  signingIn: string;
  retry: string;
}

// What a refused identity is offered: leave signs the identity provider session out (so the next sign-in is
// not the same person again), getToken fetches a fresh session token.
export interface RefusedIdentity {
  leave: () => Promise<void>;
  getToken: () => Promise<string>;
  signOutFailed: boolean;
}

export interface ClerkSessionProps {
  copy: ClerkSessionCopy;
  // onSessionToken hands the identity provider's session token to Colliedesk; isRefused tells its refusal
  // of this identity (shown by refused) from a failure worth retrying.
  onSessionToken: (token: string) => Promise<void>;
  isRefused: (error: unknown) => boolean;
  refused: (identity: RefusedIdentity) => ReactNode;
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

// ClerkSession is the identity provider's sign-in form and the hand-over of its session token, inside a
// ClerkProvider: the workspace app's cloud sign-in and the operator console's both use it.
export function ClerkSession({ copy, onSessionToken, isRefused, refused, endSession = false }: ClerkSessionProps) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const clerk = useClerk();
  const exchanged = useRef(false);
  const [failed, setFailed] = useState(false);
  const [refusal, setRefusal] = useState(false);
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
    if (ending || !isLoaded || !isSignedIn || exchanged.current || failed || refusal) return;
    exchanged.current = true;
    void getToken().then((token) => {
      if (!token) throw new Error('Clerk did not return a session token');
      return onSessionToken(token);
    }).catch((error: unknown) => {
      exchanged.current = false;
      if (isRefused(error)) setRefusal(true);
      else setFailed(true);
    });
  }, [ending, failed, getToken, isLoaded, isRefused, isSignedIn, onSessionToken, refusal]);

  const leave = useCallback(async () => {
    try {
      await clerk.signOut();
      setSignOutFailed(false);
      setRefusal(false);
    } catch {
      setSignOutFailed(true);
    }
  }, [clerk]);
  // Clerk session tokens live for about a minute, so a token for a later step is fetched when it is sent.
  const freshToken = useCallback(async () => {
    const token = await getToken();
    if (!token) throw new Error('Clerk did not return a session token');
    return token;
  }, [getToken]);

  if (ending) return signOutFailed ? <div className="cloud-signup">
    <p role="alert" className="error-copy">{copy.signOutFailed}</p>
    <button type="button" onClick={() => void endOpenSession()}>{copy.tryAgain}</button>
  </div> : <p>{copy.signingOut}</p>;
  if (refusal) return refused({ leave, getToken: freshToken, signOutFailed });
  if (failed) return <button type="button" onClick={() => setFailed(false)}>{copy.retry}</button>;
  if (!isLoaded || isSignedIn) return <p>{copy.signingIn}</p>;
  // Stay on this page after sign-in (Clerk would otherwise go to "/" and unmount this component
  // before the exchange). withSignUp lets a person who has no Clerk identity yet sign in with a
  // code; the server still admits only the emails it knows.
  const here = returnAddress();
  return <SignIn routing="hash" withSignUp forceRedirectUrl={here} signUpForceRedirectUrl={here} appearance={clerkAppearance()} />;
}
