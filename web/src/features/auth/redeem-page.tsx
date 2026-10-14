import { messages } from '../../messages';

interface RedeemPageProps {
  token: string;
  pending?: boolean;
  failed?: boolean;
  onRedeem?: () => void;
}

export function RedeemPage({ token, pending, failed, onRedeem }: RedeemPageProps) {
  return (
    <main className="redeem-page">
      <section className="redeem-panel">
        <div className="brand-mark" aria-hidden="true">{messages.brandInitial}</div>
        <h1>{messages.openSecureLink}</h1>
        <p>{messages.redeemExplanation}</p>
        {!token && <p className="muted-copy">{messages.requestLink}</p>}
        {failed && <p role="alert" className="error-copy">{messages.invalidLink}</p>}
        {token && <button className="primary-action" type="button" onClick={onRedeem} disabled={pending}>
          {pending ? messages.redeeming : messages.continue}
        </button>}
      </section>
    </main>
  );
}
