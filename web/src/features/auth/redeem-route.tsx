import { lazy, Suspense, useCallback, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api, CloudWorkspaceChoiceError, PlanLimitError } from '../../api/client';
import { NotAnAgentError } from '../../api/cloud-signup';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { readCheckoutIntent } from '../settings/checkout-intent';
import { RedeemPage } from './redeem-page';

const CloudSignIn = lazy(() => import('./cloud-sign-in').then((module) => ({ default: module.CloudSignIn })));

export function RedeemRoute({ token }: { token: string }) {
  const navigate = useNavigate();
  // A visitor who came to buy a plan lands on the billing page, which carries the purchase out.
  const goAfterSignIn = useCallback(
    () => (readCheckoutIntent() ? navigate({ to: '/settings/billing', search: { checkout: '' } }) : navigate({ to: '/' })),
    [navigate],
  );
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const [workspaceSlug, setWorkspaceSlug] = useState<string>();
  const [workspaces, setWorkspaces] = useState<WorkspaceChoice[]>([]);
  const [cloudPending, setCloudPending] = useState(false);
  const [cloudFailed, setCloudFailed] = useState(false);
  const redeem = useMutation({ mutationFn: () => api.redeem(token, navigator.userAgent), onSuccess: goAfterSignIn });
  const exchange = useCallback(async (sessionToken: string) => {
    setCloudPending(true);
    setCloudFailed(false);
    try {
      await api.exchangeCloudSession(sessionToken, workspaceSlug);
      await goAfterSignIn();
    } catch (error) {
      if (error instanceof CloudWorkspaceChoiceError) setWorkspaces(error.workspaces);
      else if (!(error instanceof NotAnAgentError)) setCloudFailed(true);
      throw error;
    } finally {
      setCloudPending(false);
    }
  }, [goAfterSignIn, workspaceSlug]);
  const signUp = useCallback(async (sessionToken: string, workspaceName: string) => {
    await api.signUpCloud(sessionToken, workspaceName);
    await goAfterSignIn();
  }, [goAfterSignIn]);
  const cloudConfig = config.data?.cloud_sign_in;
  const cloudEnabled = !token && cloudConfig?.enabled === true && cloudConfig.publishable_key !== null;

  return <RedeemPage token={token} cloudEnabled={cloudEnabled} signupEnabled={cloudEnabled && config.data?.signup.enabled === true} workspaces={workspaces}
    pending={redeem.isPending || cloudPending} failed={redeem.isError || cloudFailed} planLimited={redeem.error instanceof PlanLimitError}
    onRedeem={() => redeem.mutate()} onSelectWorkspace={(slug) => { setWorkspaces([]); setWorkspaceSlug(slug); }}
    cloudSignIn={cloudEnabled && <Suspense fallback={<p>{messages.loadingCloudSignIn}</p>}>
      <CloudSignIn key={workspaceSlug ?? 'default'} publishableKey={cloudConfig.publishable_key ?? ''} onSessionToken={exchange}
        onSignUp={config.data?.signup.enabled ? signUp : undefined} />
    </Suspense>} />;
}
