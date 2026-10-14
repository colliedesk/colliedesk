import { lazy, Suspense, useCallback, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api, CloudWorkspaceChoiceError } from '../../api/client';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { RedeemPage } from './redeem-page';

const CloudSignIn = lazy(() => import('./cloud-sign-in').then((module) => ({ default: module.CloudSignIn })));

export function RedeemRoute({ token }: { token: string }) {
  const navigate = useNavigate();
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const [workspaceSlug, setWorkspaceSlug] = useState<string>();
  const [workspaces, setWorkspaces] = useState<WorkspaceChoice[]>([]);
  const [cloudPending, setCloudPending] = useState(false);
  const [cloudFailed, setCloudFailed] = useState(false);
  const redeem = useMutation({ mutationFn: () => api.redeem(token, navigator.userAgent), onSuccess: () => navigate({ to: '/' }) });
  const exchange = useCallback(async (sessionToken: string) => {
    setCloudPending(true);
    setCloudFailed(false);
    try {
      await api.exchangeCloudSession(sessionToken, workspaceSlug);
      await navigate({ to: '/' });
    } catch (error) {
      if (error instanceof CloudWorkspaceChoiceError) setWorkspaces(error.workspaces);
      else setCloudFailed(true);
      throw error;
    } finally {
      setCloudPending(false);
    }
  }, [navigate, workspaceSlug]);
  const cloudConfig = config.data?.cloud_sign_in;
  const cloudEnabled = !token && cloudConfig?.enabled === true && cloudConfig.publishable_key !== null;

  return <RedeemPage token={token} cloudEnabled={cloudEnabled} workspaces={workspaces}
    pending={redeem.isPending || cloudPending} failed={redeem.isError || cloudFailed}
    onRedeem={() => redeem.mutate()} onSelectWorkspace={(slug) => { setWorkspaces([]); setWorkspaceSlug(slug); }}
    cloudSignIn={cloudEnabled && <Suspense fallback={<p>{messages.loadingCloudSignIn}</p>}>
      <CloudSignIn key={workspaceSlug ?? 'default'} publishableKey={cloudConfig.publishable_key ?? ''} onSessionToken={exchange} />
    </Suspense>} />;
}
