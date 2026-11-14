// What the Settings menu's dot on Channels means: a channel that was working, or whose connection was made, now has
// a problem the admin must act on. The server also says needs_attention of a channel still being set up (a web form
// with no origin yet, a help chat while its AI chat is off, a Zalo OA without its secrets, an email address whose
// domain is not verified); setup has its own place, in Quick start and on the channel's page, so it is no problem here.
// What tells the two apart is the channel's history: something arrived on it, or its connection broke.
const connectionLost = new Set(['gmail_broken', 'microsoft_broken', 'signature_failed', 'sending_paused']);

interface ListedChannel { status: { state: string; reason_code?: string | null }; last_inbound_at?: string | null }

export function channelNeedsAction(channel: ListedChannel): boolean {
  if (channel.status.state !== 'needs_attention') return false;
  return Boolean(channel.last_inbound_at) || connectionLost.has(channel.status.reason_code ?? '');
}
