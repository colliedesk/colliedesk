// Shared Cloudflare Email Worker for the hosted cloud. Every workspace address
// <anything>@<slug>.<hosted domain> routes here; the server picks the workspace from the envelope
// recipient this Worker writes at the top of the message as X-Original-To.
//
// Secrets: INBOUND_URL, INBOUND_SECRET (the server's MAIL_FROM_INBOUND_SECRET).
// Variables: FALLBACK_ADDRESS (a verified destination address that keeps mail the server could not
// take), RETRY_DELAY_MS (default 500).

const ATTEMPTS = 3;
// The server answers these when retrying cannot help: not an email with a sender, a locked
// workspace that no longer takes mail, no workspace at that address, larger than 25 MiB.
const PERMANENT = new Set([400, 403, 404, 413]);

const REJECT_TEXT = {
  403: 'This mailbox is not accepting new mail.',
  413: 'The message is too large.',
};

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function post(env, body) {
  try {
    const response = await fetch(env.INBOUND_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': env.INBOUND_SECRET },
      body,
    });
    return response.status;
  } catch {
    return 0;
  }
}

// Mail the server could not take for now goes to a person when FALLBACK_ADDRESS is set; otherwise it
// is rejected, so the sender gets a bounce instead of losing the message silently.
async function keepOrReject(message, env, reason) {
  if (env.FALLBACK_ADDRESS) {
    await message.forward(env.FALLBACK_ADDRESS);
    return;
  }
  message.setReject(reason);
}

export default {
  async email(message, env) {
    if (!env.INBOUND_URL || !env.INBOUND_SECRET) {
      await keepOrReject(message, env, 'Mail for this address cannot be accepted right now.');
      return;
    }
    // The envelope recipient goes first: the server ignores To and Cc, which the sender writes.
    const envelope = new TextEncoder().encode(`X-Original-To: ${message.to}\r\n`);
    const body = await new Blob([envelope, await new Response(message.raw).arrayBuffer()]).arrayBuffer();
    const delay = Number.parseInt(env.RETRY_DELAY_MS ?? '500', 10);
    for (let attempt = 1; attempt <= ATTEMPTS; attempt += 1) {
      const status = await post(env, body);
      if (status >= 200 && status < 300) return;
      if (PERMANENT.has(status)) {
        message.setReject(REJECT_TEXT[status] ?? 'There is no mailbox at this address.');
        return;
      }
      if (attempt < ATTEMPTS) await sleep(delay * attempt);
    }
    await keepOrReject(message, env, 'Mail for this address cannot be accepted right now. Try again later.');
  },
};
