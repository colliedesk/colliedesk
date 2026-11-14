import { ClerkProvider, useClerk } from '@clerk/clerk-react';
import { Component, useCallback, useEffect, useState } from 'react';
import type { ReactNode } from 'react';

export interface ClerkLoadCopy {
  notLoaded: string;
  retry: string;
}

// A throw while Clerk renders (it refuses a malformed publishable key at once) is reported, never passed
// up to the router, which would replace the whole page.
class RenderFailure extends Component<{ onFailed: () => void; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch() { this.props.onFailed(); }
  render() { return this.state.failed ? null : this.props.children; }
}

// Clerk that cannot download or start its script (a blocker, a network that cannot reach it, a key of
// another instance) sets its status to error and stays there.
function LoadFailure({ onFailed }: { onFailed: () => void }) {
  const { status } = useClerk();
  useEffect(() => { if (status === 'error') onFailed(); }, [onFailed, status]);
  return null;
}

// ClerkLoadGuard is the ClerkProvider of a sign-in form: when Clerk cannot start, only the form says so,
// with a retry. Retry unmounts the provider first: Clerk keeps one instance until its provider unmounts,
// so a provider mounted again starts a new one and downloads the script again.
export function ClerkLoadGuard({ publishableKey, copy, children }: { publishableKey: string; copy: ClerkLoadCopy; children: ReactNode }) {
  const [failed, setFailed] = useState(false);
  const onFailed = useCallback(() => setFailed(true), []);
  if (failed) return <div className="cloud-signup">
    <p role="alert" className="error-copy">{copy.notLoaded}</p>
    <button type="button" onClick={() => setFailed(false)}>{copy.retry}</button>
  </div>;
  return <RenderFailure onFailed={onFailed}>
    <ClerkProvider publishableKey={publishableKey}>
      <LoadFailure onFailed={onFailed} />
      {children}
    </ClerkProvider>
  </RenderFailure>;
}
