import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError, UnauthorizedError } from '../../api/client';
import type { TrialOffer } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { LogoMark } from '../../ui/logo';
import { messages } from '../../messages';
import { namesList, trialOfferMessages as text } from './trial-offer-messages';
import './auth.css';

interface TrialOfferPageProps {
  // token is the offer link's token; here is this page's own address (path and query), where a sign-in returns.
  token: string;
  here: string;
}

// TrialOfferPage is where a trial offer link an operator emailed opens. Opening it changes nothing (mail
// scanners open links too): only an admin of that workspace pressing Continue trial does. A locked
// workspace reaches it too, so it has no rail and checks its own session, like the OAuth consent page.
export function TrialOfferPage({ token, here }: TrialOfferPageProps) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, retry: false });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const offer = useQuery({ queryKey: ['trial-offer', token], queryFn: () => api.trialOffer(token), enabled: Boolean(me.data) && token !== '', retry: false });
  // The workspace opens at once: the session's plan is read again.
  const accept = useMutation({ mutationFn: () => api.acceptTrialOffer(token, crypto.randomUUID()), onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  const issuer = config.data?.app_origin || window.location.origin;
  const cloud = config.data?.cloud_sign_in.enabled === true;
  const signInAddress = `${issuer}/sign-in?${new URLSearchParams({ next: here }).toString()}`;
  const timeZone = me.data?.timezone ?? 'UTC';
  const when = (value: string) => formatDate(value, timeZone);

  let body;
  if (me.isPending || config.isPending || (me.data && offer.isPending && token !== '')) {
    body = <p>{text.loading}</p>;
  } else if (me.error instanceof UnauthorizedError || !me.data) {
    body = <><h1>{text.signInTitle}</h1>
      <p>{cloud ? text.signInCloud : text.signInSelfHost}</p>
      <div className="consent-actions">
        {cloud ? <a className="primary-action" href={signInAddress}>{text.signIn}</a>
          : <button type="button" className="primary-action" onClick={() => void me.refetch()}>{text.tryAgain}</button>}
      </div></>;
  } else if (offer.error || !offer.data) {
    const refused = offer.error instanceof InvalidRequestError ? offer.error : undefined;
    body = <><h1>{text.refusedTitle}</h1>
      <p role="alert" className="error-copy">{refused?.message ?? text.failed}</p>
      {refused?.detail?.code === 'trial_offer_other_workspace' && cloud && <p className="muted-copy"><a href={signInAddress}>{text.otherWorkspace}</a></p>}</>;
  } else if (accept.data) {
    const ends = when(accept.data.trial_ends_at);
    body = <><h1>{text.title(accept.data.offer.workspace_name)}</h1>
      <p role="status">{accept.data.extended ? text.accepted(ends) : text.kept(ends)}</p>
      <div className="consent-actions"><a className="primary-action" href="/">{text.open}</a></div></>;
  } else {
    body = <OfferBody offer={offer.data} when={when} pending={accept.isPending} onAccept={() => accept.mutate()}
      failed={accept.error ? (accept.error instanceof InvalidRequestError ? accept.error.message : text.acceptFailed) : undefined} />;
  }
  return <main className="redeem-page"><section className="redeem-panel oauth-consent" aria-live="polite">
    <div className="brand-lockup"><LogoMark size={36} /><span>{messages.brand}</span></div>
    {body}
  </section></main>;
}

function OfferBody({ offer, when, pending, failed, onAccept }: { offer: TrialOffer; when: (value: string) => string; pending: boolean; failed?: string; onAccept: () => void }) {
  const ended = new Date(offer.trial_ends_at).getTime() <= Date.now();
  const trialEnd = <p>{(ended ? text.trialEnded : text.trialEnds)(when(offer.trial_ends_at))}</p>;
  const support = <p className="muted-copy">{text.moreTime} <a href={`mailto:${text.supportEmail}`}>{text.supportEmail}</a>.</p>;
  const title = <h1>{text.title(offer.workspace_name)}</h1>;
  switch (offer.refusal) {
    case 'accepted':
    case 'expired':
    case 'revoked':
      return <>{title}<p role="alert" className="error-copy">{text.refusals[offer.refusal]}</p>{trialEnd}{support}</>;
    case 'has_plan':
      return <>{title}<p role="status">{text.hasPlan(offer.workspace_name)}</p></>;
    case 'not_admin':
      return <>{title}<p role="alert" className="error-copy">{offer.accept_by.length > 0 ? text.notAdmin(namesList(offer.accept_by)) : text.notAdminNoOne}</p></>;
  }
  return <>{title}
    <p>{text.offer}</p>
    {failed && <p role="alert" className="error-copy">{failed}</p>}
    <div className="consent-actions">
      <button type="button" className="primary-action" disabled={pending} onClick={onAccept}>{pending ? text.accepting : text.accept}</button>
    </div></>;
}
