import { useQuery } from '@tanstack/react-query';
import { channelsApi } from '../../api/channels';
import { clockTime } from './setup-state';
import { setupMessages as m } from './setup-messages';

// How often the open step asks whether Gmail's confirmation arrived.
const pollMilliseconds = 5_000;

// GmailConfirmation shows the confirmation Gmail sends when the forwarding address is added, so the person
// confirms it here instead of searching the inbox. It asks every few seconds only while it is on screen and
// the step still waits for its first email; the inbox poll asks once more when that email comes. The link
// is only ever Google's, checked by the server; otherwise the code and the conversation it opened show.
export function GmailConfirmation({ channelID, timezone, waiting }: { channelID: string; timezone: string; waiting: boolean }) {
  const confirmation = useQuery({
    queryKey: ['forwarding-confirmation', channelID],
    queryFn: () => channelsApi.emailForwardingConfirmation(channelID),
    retry: false,
    refetchInterval: (query) => (query.state.data || !waiting ? false : pollMilliseconds),
  });
  const found = confirmation.data;
  return <section className={`setup-confirmation${found ? ' setup-confirmation-ok' : ''}`} aria-labelledby="gmail-confirmation-heading">
    <span className={`setup-pill${found ? '' : ' setup-pill-wait'}`}>{found ? m.arrived : m.waiting}</span>
    <div>
      <h4 id="gmail-confirmation-heading">{m.confirmationTitle}</h4>
      {found
        ? <>
            <p><strong>{m.confirmationCode} {found.code}</strong></p>
            <p className="muted-copy">{m.confirmationFrom(clockTime(found.received_at, timezone))}</p>
            <p className="setup-confirmation-links">
              {found.confirm_url && <a className="button primary-action" href={found.confirm_url} target="_blank" rel="noreferrer">{m.openConfirm} ↗</a>}
              <a href={`/conversations/${encodeURIComponent(found.conversation_id)}`}>{m.openConversation}</a>
            </p>
          </>
        : <p className="muted-copy">{m.confirmationWaiting}</p>}
    </div>
  </section>;
}
