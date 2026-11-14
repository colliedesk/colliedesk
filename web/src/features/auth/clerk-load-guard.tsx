import { ClerkProvider, useClerk } from '@clerk/clerk-react';
import { Component, useCallback, useEffect, useState } from 'react';
import type { ReactNode } from 'react';

export interface ClerkLoadCopy {
  // Clerk could not download or start its script: the visitor's connection or blocker, so a retry can help.
  notLoaded: string;
  // The server's publishable key is missing or malformed: only whoever runs the server can fix it.
  notSetUp: string;
  retry: string;
}

// ClerkProvider throws while rendering only when the publishable key is missing or malformed, with
// messages of its own error thrower ("@clerk/clerk-react: Missing publishableKey…", "…: The publishableKey
// passed to Clerk is invalid…").
const clerkKeyError = /^@clerk\/clerk-react: (Missing publishableKey|The publishableKey passed to Clerk is invalid)/;

function isClerkKeyError(error: unknown): boolean {
  return error instanceof Error && clerkKeyError.test(error.message);
}

// KeyFailure reports Clerk refusing the server's key, which the router would turn into a whole-page error.
// Any other throw (a bug in the sign-in form under it) is thrown on, so the app's own error screen and
// diagnostics get it as they would without this guard.
class KeyFailure extends Component<{ onFailed: () => void; children: ReactNode }, { error?: unknown }> {
  state: { error?: unknown } = {};
  static getDerivedStateFromError(error: unknown) { return { error }; }
  componentDidCatch(error: unknown) { if (isClerkKeyError(error)) this.props.onFailed(); }
  render() {
    if (this.state.error === undefined) return this.props.children;
    if (isClerkKeyError(this.state.error)) return null;
    throw this.state.error;
  }
}

// Clerk that cannot download or start its script (a blocker, a network that cannot reach it, a key of
// another instance) sets its status to error and stays there.
function LoadFailure({ onFailed }: { onFailed: () => void }) {
  const { status } = useClerk();
  useEffect(() => { if (status === 'error') onFailed(); }, [onFailed, status]);
  return null;
}

type Failure = 'notLoaded' | 'notSetUp';

// ClerkLoadGuard is the ClerkProvider of a sign-in form: when Clerk cannot start, only the form says so.
// A script that could not load gets a retry. Retry unmounts the provider first: Clerk keeps one instance
// until its provider unmounts, so a provider mounted again starts a new one and downloads the script again.
// A key Clerk refuses gets no retry, since the same key fails the same way.
export function ClerkLoadGuard({ publishableKey, copy, children }: { publishableKey: string; copy: ClerkLoadCopy; children: ReactNode }) {
  const [failure, setFailure] = useState<Failure>();
  const notLoaded = useCallback(() => setFailure('notLoaded'), []);
  const notSetUp = useCallback(() => setFailure('notSetUp'), []);
  if (failure === 'notSetUp') return <div className="cloud-signup"><p role="alert" className="error-copy">{copy.notSetUp}</p></div>;
  if (failure === 'notLoaded') return <div className="cloud-signup">
    <p role="alert" className="error-copy">{copy.notLoaded}</p>
    <button type="button" onClick={() => setFailure(undefined)}>{copy.retry}</button>
  </div>;
  return <KeyFailure onFailed={notSetUp}>
    <ClerkProvider publishableKey={publishableKey}>
      <LoadFailure onFailed={notLoaded} />
      {children}
    </ClerkProvider>
  </KeyFailure>;
}
