import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { markSignInFormReady } from '../../lib/sign-in-ready';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api, CloudWorkspaceChoiceError, PlanLimitError } from '../../api/client';
import { NotAnAgentError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import type { WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { leaveTo } from '../../lib/leave-to';
import { readCheckoutIntent } from '../settings/checkout-intent';
import { preconnectClerk } from './clerk-appearance';
import { RedeemPage } from './redeem-page';

const loadCloudSignIn = () => import('./cloud-sign-in');
const CloudSignIn = lazy(() => loadCloudSignIn().then((module) => ({ default: module.CloudSignIn })));

// buy is the plan named in the sign-in address when a visitor came from the website to buy it.
// signedOut marks the arrival right after the browser's session ended. trial marks the website's
// "Start free trial" link. On APP_ORIGIN of a server whose workspaces each have a host of their own, a
// completed sign-in continues on the workspace's host; on a workspace host, the server signs in to that
// workspace only, and an email that is not an agent there is pointed to APP_ORIGIN to find its workspace.
export function RedeemRoute({ token, buy, trial = false, signedOut = false }: { token: string; buy?: string; trial?: boolean; signedOut?: boolean }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  // A visitor who came to buy a plan lands on the billing page, which carries the purchase out.
  // The new session starts from a fresh me: the refused answer of the ended one would send the app
  // straight back here.
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  // On APP_ORIGIN of a server with workspace hosts the server answers where the browser continues
  // (continue_url): the workspace's own host, signed in there. landing is where a sign-in without a plan
  // opens: the inbox, or the setup flow for a team that just signed up.
  const goAfterSignIn = useCallback(async (result?: { continue_url?: string }, landing: '/' | '/setup' = '/') => {
    if (result?.continue_url) {
      leaveTo(result.continue_url);
      return undefined;
    }
    await client.resetQueries({ queryKey: ['me'] });
    return readCheckoutIntent() ? navigate({ to: '/settings/billing', search: { checkout: '' } }) : navigate({ to: landing });
  }, [client, navigate]);
  // The page a sign-in here opens on the workspace host. A plan to buy rides in it: that host's storage
  // is not this page's.
  const plan = readCheckoutIntent();
  const next = plan ? `/settings/billing?buy=${encodeURIComponent(plan)}` : undefined;
  // The website's trial and buy links only lead to cloud sign-in: its chunk downloads beside the config
  // instead of after it. A page without a sign-in form lets what waited for one go.
  useEffect(() => { if (!token && (trial || buy)) void loadCloudSignIn().catch(() => undefined); }, [token, trial, buy]);
  const [workspaceSlug, setWorkspaceSlug] = useState<string>();
  const [workspaces, setWorkspaces] = useState<WorkspaceChoice[]>([]);
  const [cloudPending, setCloudPending] = useState(false);
  const [cloudFailed, setCloudFailed] = useState(false);
  const redeem = useMutation({ mutationFn: () => api.redeem(token, navigator.userAgent, next), onSuccess: (result) => goAfterSignIn(result) });
  const exchange = useCallback(async (sessionToken: string) => {
    setCloudPending(true);
    setCloudFailed(false);
    try {
      await goAfterSignIn(await api.exchangeCloudSession(sessionToken, workspaceSlug, next));
    } catch (error) {
      if (error instanceof CloudWorkspaceChoiceError) setWorkspaces(error.workspaces);
      else if (!(error instanceof NotAnAgentError)) setCloudFailed(true);
      throw error;
    } finally {
      setCloudPending(false);
    }
  }, [goAfterSignIn, next, workspaceSlug]);
  // A new team opens the setup flow, also when the sign-up continues on the workspace's own host.
  const signUp = useCallback(async (sessionToken: string, workspaceName: string, answers: SignupAnswers) => {
    await goAfterSignIn(await api.signUpCloud(sessionToken, workspaceName, answers, next ?? '/setup'), '/setup');
  }, [goAfterSignIn, next]);
  const cloudConfig = config.data?.cloud_sign_in;
  const cloudEnabled = !token && cloudConfig?.enabled === true && cloudConfig.publishable_key !== null;
  useEffect(() => { if (config.data && !cloudEnabled) markSignInFormReady(); }, [config.data, cloudEnabled]);
  useEffect(() => { if (cloudEnabled && cloudConfig?.publishable_key) preconnectClerk(cloudConfig.publishable_key); }, [cloudEnabled, cloudConfig?.publishable_key]);
  const buying = buy ?? readCheckoutIntent();
  const appOrigin = config.data?.app_origin ?? '';
  // On a workspace's own host a new team cannot start here: sign-up and finding a workspace are on APP_ORIGIN.
  const findWorkspaceAt = config.data?.workspace_hosts && appOrigin !== '' && appOrigin !== window.location.origin ? `${appOrigin}/sign-in` : undefined;

  return <RedeemPage token={token} cloudEnabled={cloudEnabled} signupEnabled={cloudEnabled && config.data?.signup.enabled === true} workspaces={workspaces}
    buying={buying} trial={trial} signedOut={signedOut && cloudEnabled} pending={redeem.isPending || cloudPending} failed={redeem.isError || cloudFailed} planLimited={redeem.error instanceof PlanLimitError}
    onRedeem={() => redeem.mutate()} onSelectWorkspace={(slug) => { setWorkspaces([]); setWorkspaceSlug(slug); }}
    cloudSignIn={cloudEnabled && <Suspense fallback={<p>{messages.loadingCloudSignIn}</p>}>
      <CloudSignIn key={workspaceSlug ?? 'default'} publishableKey={cloudConfig.publishable_key ?? ''} onSessionToken={exchange}
        onSignUp={config.data?.signup.enabled && !findWorkspaceAt ? signUp : undefined} findWorkspaceAt={findWorkspaceAt} buying={buying !== undefined} endSession={signedOut} />
    </Suspense>} />;
}
