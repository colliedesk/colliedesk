import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { localPath } from '../../lib/workspace-host';
import { messages } from '../../messages';

// HandoffSignIn finishes the move from APP_ORIGIN to this workspace's host: the one-time token from the
// address's fragment (which no server saw) becomes this host's session, and the page named by next
// opens. A token that does not sign in here explains itself and offers the sign-in page.
export function HandoffSignIn({ token, next }: { token: string; next?: string }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const started = useRef(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void api.redeem(token, navigator.userAgent).then(async () => {
      await client.resetQueries({ queryKey: ['me'] });
      await navigate({ href: localPath(next), replace: true });
    }).catch(() => setFailed(true));
  }, [client, navigate, next, token]);
  if (!failed) return <p>{messages.loadingCloudSignIn}</p>;
  return <main className="redeem-page"><section className="redeem-panel">
    <p role="alert" className="error-copy">{messages.handoffFailed}</p>
    <Link to="/sign-in">{messages.cloudSignInTitle}</Link>
  </section></main>;
}
