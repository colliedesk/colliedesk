import type { InvitePreview } from '../../api/schemas';
import { browserTimeZone, formatDate, formatMonthDay } from '../../lib/format-date';
import { messages } from '../../messages';
import { LogoMark } from '../../ui/logo';
import { inviteMessages as copy } from './invite-messages';
import './auth.css';

interface InvitePageProps {
  preview: InvitePreview;
  // cloud is true where a person signs in with a code sent to their email; elsewhere only by links.
  cloud: boolean;
  pending?: boolean;
  // planLimited: redeem answered that the workspace has no free seat; the link stays usable.
  planLimited?: boolean;
  // failed: redeem refused the link for another reason.
  failed?: boolean;
  onJoin?: () => void;
  // The person is not signed in yet, so dates read in the browser's own timezone.
  timeZone?: string;
  now?: Date;
}

// The invite page (Paper "05 Access"): A2-a joins a workspace, A2-b says it has no free seat, A2-c says why
// the link no longer works. A pairing link, a hand-off and an unknown token keep RedeemPage.
export function InvitePage({ preview, cloud, pending = false, planLimited = false, failed = false, onJoin, timeZone = browserTimeZone(), now = new Date() }: InvitePageProps) {
  const workspace = preview.workspace_name;
  const inviter = preview.invited_by;
  const short = inviter.includes('@') ? inviter : inviter.split(/\s+/)[0];
  const until = formatDate(preview.expires_at, timeZone, now).replace(/\s/g, ' ');
  return (
    <main className="redeem-page invite-page">
      <section className="redeem-panel">
        <div className="brand-lockup"><LogoMark size={20} /><span>{messages.brand}</span></div>
        {preview.state === 'pending' ? <>
          <h1>{copy.joinTitle(workspace)}</h1>
          <p>{copy.invitedBy(inviter)}</p>
          {planLimited ? <p role="status" className="warn-copy">{copy.noSeat(workspace, inviter, until)}</p> : <ul className="invite-facts" aria-label={copy.factsLabel}>
            <li><span>{copy.yourEmail}</span><span>{preview.email}</span></li>
            <li><span>{copy.role(preview.roles.length)}</span><span>{preview.roles.join(', ')}</span></li>
            <li><span>{copy.expires}</span><span>{until}</span></li>
          </ul>}
          {failed && !planLimited && <p role="alert" className="error-copy">{messages.invalidLink}</p>}
          <button className="primary-action" type="button" onClick={onJoin} disabled={pending}>
            {pending ? copy.joining : planLimited ? copy.tryAgain : copy.join(workspace)}
          </button>
          {!planLimited && <p className="invite-footnote">{cloud ? copy.footnoteCloud(preview.email, short) : copy.footnoteLinks(short)}</p>}
        </> : <>
          <h1>{copy.noLongerWorks}</h1>
          <p>{preview.state === 'expired' ? copy.expired(workspace, formatMonthDay(preview.expires_at, timeZone, now), inviter)
            : preview.state === 'revoked' ? copy.revoked(workspace, inviter)
              : cloud ? copy.usedCloud(workspace) : copy.usedLinks(workspace, inviter)}</p>
          {/* The app's root signs a browser in where it can: the inbox with a session, else the sign-in page. */}
          {preview.state === 'used'
            ? <a className="primary-action" href="/">{copy.signIn}</a>
            : <a className="secondary-action" href="/">{copy.alreadyJoined}</a>}
        </>}
      </section>
    </main>
  );
}

// LinkPageLoading holds the card while the page asks what a link is, so a person never reads one page's
// words before the other's.
export function LinkPageLoading() {
  return (
    <main className="redeem-page invite-page" aria-busy="true">
      <section className="redeem-panel"><div className="brand-lockup"><LogoMark size={20} /><span>{messages.brand}</span></div></section>
    </main>
  );
}
