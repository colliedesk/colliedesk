import { ClerkProvider, useAuth } from '@clerk/clerk-react';
import { useEffect, useRef } from 'react';
import { api } from '../../api/client';
import type { WorkspaceChoice } from '../../api/schemas';

interface OtherWorkspacesProps {
  publishableKey: string;
  // onLoaded receives the verified identity's workspaces: none without an identity provider session, or
  // when they cannot be read (the menu then offers no switch, and Sign out still works).
  onLoaded: (workspaces: WorkspaceChoice[]) => void;
}

// OtherWorkspaces reads, in its own chunk, which workspaces the identity provider's signed-in person belongs
// to. The proof is the identity provider's token: the workspace session's address is not proven, since an
// admin can invite any address. It renders nothing.
export function OtherWorkspaces({ publishableKey, onLoaded }: OtherWorkspacesProps) {
  return <ClerkProvider publishableKey={publishableKey}><Load onLoaded={onLoaded} /></ClerkProvider>;
}

// Load asks once per identity provider session: getToken is read through a ref, so a new function identity
// never asks again.
function Load({ onLoaded }: Pick<OtherWorkspacesProps, 'onLoaded'>) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const token = useRef(getToken);
  token.current = getToken;
  useEffect(() => {
    if (!isLoaded) return undefined;
    if (!isSignedIn) {
      onLoaded([]);
      return undefined;
    }
    let current = true;
    void token.current()
      .then((value) => (value ? api.listCloudWorkspaces(value) : []))
      .catch(() => [])
      .then((workspaces) => { if (current) onLoaded(workspaces); });
    return () => { current = false; };
  }, [isLoaded, isSignedIn, onLoaded]);
  return null;
}
