import { ClerkProvider, useAuth } from '@clerk/clerk-react';
import { useEffect, useRef } from 'react';
import { api } from '../../api/client';
import type { WorkspaceChoice } from '../../api/schemas';

// Why the list could not be read. Each is logged, so a report from the browser console says which step failed.
export type WorkspacesFailure = 'not-loaded' | 'not-signed-in' | 'no-token' | 'request-failed' | 'chunk-failed';
export type WorkspacesResult = { status: 'loaded'; workspaces: WorkspaceChoice[] } | { status: 'failed'; reason: WorkspacesFailure };

// How long the identity provider may take to load before the dialog says the list could not be read.
export const identityLoadTimeoutMs = 10_000;

interface OtherWorkspacesProps {
  publishableKey: string;
  // onResult receives the verified identity's workspaces, or why they could not be read: never an empty
  // list in place of a failure, which would look like having no other workspace.
  onResult: (result: WorkspacesResult) => void;
}

export function warnWorkspacesFailed(reason: WorkspacesFailure, error?: unknown) {
  if (error === undefined) console.warn('Switch workspace: could not load the workspaces', reason);
  else console.warn('Switch workspace: could not load the workspaces', reason, error);
}

// OtherWorkspaces reads, in its own chunk, which workspaces the identity provider's signed-in person belongs
// to. The proof is the identity provider's token: the workspace session's address is not proven, since an
// admin can invite any address. It renders nothing.
export function OtherWorkspaces({ publishableKey, onResult }: OtherWorkspacesProps) {
  return <ClerkProvider publishableKey={publishableKey}><Load onResult={onResult} /></ClerkProvider>;
}

// Load asks once per mount (the dialog remounts it to try again): getToken and onResult are read through refs,
// so a new function identity never asks again.
function Load({ onResult }: Pick<OtherWorkspacesProps, 'onResult'>) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const token = useRef(getToken);
  token.current = getToken;
  const report = useRef(onResult);
  report.current = onResult;
  useEffect(() => {
    if (isLoaded) return undefined;
    const timer = window.setTimeout(() => {
      warnWorkspacesFailed('not-loaded');
      report.current({ status: 'failed', reason: 'not-loaded' });
    }, identityLoadTimeoutMs);
    return () => window.clearTimeout(timer);
  }, [isLoaded]);
  useEffect(() => {
    if (!isLoaded) return undefined;
    if (!isSignedIn) {
      warnWorkspacesFailed('not-signed-in');
      report.current({ status: 'failed', reason: 'not-signed-in' });
      return undefined;
    }
    let current = true;
    const fail = (reason: WorkspacesFailure, error?: unknown) => {
      if (!current) return;
      warnWorkspacesFailed(reason, error);
      report.current({ status: 'failed', reason });
    };
    void token.current().then(async (value) => {
      if (!value) return fail('no-token');
      try {
        const workspaces = await api.listCloudWorkspaces(value);
        if (current) report.current({ status: 'loaded', workspaces });
      } catch (error) {
        fail('request-failed', error);
      }
      return undefined;
    }, (error: unknown) => fail('no-token', error));
    return () => { current = false; };
  }, [isLoaded, isSignedIn]);
  return null;
}
