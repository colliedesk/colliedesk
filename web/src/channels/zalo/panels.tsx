import type { ChannelPanelProps } from '../registry';

// Until the Zalo pages are designed, an OA is connected through the API (docs/user/zalo.md).
const m = {
  how: 'A Zalo OA is connected through the API for now: see the Zalo OA guide.',
  oa: 'OA id',
  webhook: 'Webhook path',
};

export function Connect() {
  return <p className="muted-copy">{m.how}</p>;
}

function Setup({ channel }: ChannelPanelProps) {
  const config = channel.config as { oa_id?: string; webhook_path?: string };
  return <dl>
    <dt>{m.oa}</dt><dd>{config.oa_id ?? '—'}</dd>
    <dt>{m.webhook}</dt><dd><code>{config.webhook_path ?? '—'}</code></dd>
  </dl>;
}

export const Tabs = { setup: Setup };
