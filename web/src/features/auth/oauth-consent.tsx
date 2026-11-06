import { useMutation, useQuery } from '@tanstack/react-query';
import { api, InvalidRequestError, UnauthorizedError } from '../../api/client';
import type { OAuthClient } from '../../api/schemas';
import { leaveTo } from '../../lib/leave-to';
import { LogoMark } from '../../ui/logo';
import { messages } from '../../messages';
import { oauthConsentMessages as text } from './oauth-consent-messages';

// The parameters an AI app sends to /oauth/authorize (RFC 6749 §4.1.1, PKCE, RFC 8707).
export interface OAuthRequest {
  client_id: string;
  redirect_uri: string;
  response_type: string;
  code_challenge: string;
  code_challenge_method: string;
  state: string;
  resource: string;
  scope: string;
}

export function oauthRequestFrom(search: URLSearchParams): OAuthRequest {
  const get = (name: string) => search.get(name) ?? '';
  return {
    client_id: get('client_id'), redirect_uri: get('redirect_uri'), response_type: get('response_type'), code_challenge: get('code_challenge'),
    code_challenge_method: get('code_challenge_method'), state: get('state'), resource: get('resource'), scope: get('scope'),
  };
}

// cancelAddress is the app's redirect (one the server confirmed is the app's own) with access_denied.
export function cancelAddress(app: OAuthClient, state: string, issuer: string): string {
  const target = new URL(app.redirect_uri);
  target.searchParams.set('error', 'access_denied');
  if (state) target.searchParams.set('state', state);
  target.searchParams.set('iss', issuer);
  return target.toString();
}

interface OAuthConsentProps {
  request: OAuthRequest;
  // here is this page's own address (path and query), where a sign-in returns.
  here: string;
}

// OAuthConsent is the page an AI app opens to connect: which app, which workspace, as whom; Allow or Cancel.
// Nothing new signs in here: the person's session of this workspace decides, and Cloud signs in through
// the sign-in page (which chooses the workspace) and comes back.
export function OAuthConsent({ request, here }: OAuthConsentProps) {
  const app = useQuery({ queryKey: ['oauth-client', request.client_id, request.redirect_uri], queryFn: () => api.describeOAuthClient(request.client_id, request.redirect_uri), retry: false });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, retry: false });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const allow = useMutation({ mutationFn: () => api.allowOAuthClient(request), onSuccess: ({ redirect_to }) => leaveTo(redirect_to) });
  const issuer = config.data?.app_origin || window.location.origin;
  const cloud = config.data?.cloud_sign_in.enabled === true;
  const signInAddress = `${issuer}/sign-in?${new URLSearchParams({ next: here }).toString()}`;

  let body;
  if (!request.client_id || !request.redirect_uri) {
    body = <><h1>{text.badRequestTitle}</h1><p role="alert" className="error-copy">{text.badRequest}</p></>;
  } else if (app.error) {
    body = <><h1>{text.badRequestTitle}</h1><p role="alert" className="error-copy">{app.error instanceof InvalidRequestError ? app.error.message : text.badRequest}</p></>;
  } else if (app.isPending || me.isPending || config.isPending) {
    body = <p>{text.loading}</p>;
  } else if (me.error instanceof UnauthorizedError || !me.data) {
    body = <><h1>{text.signInTitle}</h1>
      <p>{cloud ? text.signInCloud : text.signInSelfHost}</p>
      {cloud ? <a className="primary-action" href={signInAddress}>{text.signIn}</a>
        : <button type="button" className="primary-action" onClick={() => void me.refetch()}>{text.tryAgain}</button>}
      <button type="button" className="secondary-action" onClick={() => leaveTo(cancelAddress(app.data, request.state, issuer))}>{text.cancel}</button></>;
  } else {
    const person = me.data.agent.display_name?.trim() || me.data.agent.email;
    const locked = me.data.plan?.locked === true;
    body = <><h1>{text.title(app.data.name)}</h1>
      <p>{text.acts(me.data.workspace.name, person)}</p>
      <p className="muted-copy">{text.changes}</p>
      {!app.data.verified && <p className="muted-copy">{text.unverifiedName}</p>}
      <p className="muted-copy">{text.returnsTo(app.data.host)}</p>
      {locked && <p role="alert" className="error-copy">{text.locked}</p>}
      {allow.error && <p role="alert" className="error-copy">{allow.error instanceof InvalidRequestError ? allow.error.message : text.allowFailed}</p>}
      <div className="consent-actions">
        <button type="button" className="primary-action" disabled={allow.isPending || locked} onClick={() => allow.mutate()}>{allow.isPending ? text.allowing : text.allow}</button>
        <button type="button" className="secondary-action" disabled={allow.isPending} onClick={() => leaveTo(cancelAddress(app.data, request.state, issuer))}>{text.cancel}</button>
      </div>
      {cloud ? <p className="muted-copy"><a href={signInAddress}>{text.otherWorkspace}</a></p> : <p className="muted-copy">{text.otherWorkspaceSelfHost}</p>}</>;
  }
  return <main className="redeem-page"><section className="redeem-panel" aria-live="polite">
    <div className="brand-lockup"><LogoMark size={36} /><span>{messages.brand}</span></div>
    {body}
  </section></main>;
}
