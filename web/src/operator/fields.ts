// How each setting shows on its page: a label people read, the control, and the section it sits in. The keys
// and what saving them asks for come from the API (GET /api/v1/operator/config); this only words them.
export type Kind = 'text' | 'number' | 'textarea' | 'switch' | 'select' | 'secret' | 'pem';

export interface FieldInfo {
  label: string;
  kind: Kind;
  help?: string;
  options?: Array<[string, string]>;
}

export interface Section {
  title: string;
  lead: string;
  keys: string[];
  test?: 'smtp' | 'polar' | 'jwks';
}

export const fields: Record<string, FieldInfo> = {
  'ai.server.provider': { label: 'Provider', kind: 'select', options: [['', 'Off'], ['anthropic', 'Anthropic'], ['openai_compatible', 'OpenAI-compatible']] },
  'ai.server.api_key': { label: 'API key', kind: 'secret' },
  'ai.server.base_url': { label: 'Base URL', kind: 'text', help: "Leave empty for the provider's own address; an OpenAI-compatible server needs its version path." },
  'ai.server.models': { label: 'Models by feature', kind: 'text', help: 'triage=model,draft=model|other; the first of each is the default.' },
  'ai.server.label': { label: 'Name shown to workspaces', kind: 'text' },
  'ai.server.prices': { label: 'Prices per million tokens', kind: 'textarea', help: 'model=input:output[:cache_read[:cache_write]], comma separated, in US dollars.' },
  'ai.server.monthly_allowance': { label: 'Monthly allowance per workspace', kind: 'text', help: 'US dollars, or agent=3,trial=2. Empty: no limit.' },
  'smtp.host': { label: 'SMTP server', kind: 'text', help: 'Empty: mail waits in the outbox and nothing is lost.' },
  'smtp.port': { label: 'Port', kind: 'number', help: '587 with STARTTLS; port 465 is not supported.' },
  'smtp.username': { label: 'Username', kind: 'text' },
  'smtp.password': { label: 'Password', kind: 'secret' },
  'smtp.starttls': { label: 'Use STARTTLS', kind: 'switch', help: 'Off sends the password in plain text.' },
  'email.from_policy': { label: 'Who a workspace may send as', kind: 'select', options: [['verified', 'Verified addresses only'], ['any', 'Any address (own relay)']] },
  'mail_from.domain': { label: 'Reply domain', kind: 'text', help: 'Replies to reply+token@ this domain thread back into their conversation.' },
  'mail_from.inbound_secret': { label: 'Inbound secret', kind: 'secret', help: 'At least 32 characters; the forwarder of the reply domain sends it.' },
  'email.trusted_authserv_id': { label: 'Trusted authentication server', kind: 'text', help: 'Whose SPF, DKIM and DMARC results are believed.' },
  'hosted_mail.domain': { label: 'Hosted mail domain', kind: 'text', help: 'Hosted cloud only: support@<workspace>.<this domain>.' },
  'resend.webhook_secret': { label: 'Resend webhook secret', kind: 'secret' },
  'resend.api_key': { label: 'Resend API key', kind: 'secret' },
  'google.client_id': { label: 'Google client ID', kind: 'text', help: 'Off while empty.' },
  'google.client_secret': { label: 'Google client secret', kind: 'secret' },
  'microsoft.client_id': { label: 'Microsoft client ID', kind: 'text', help: 'Off while empty.' },
  'microsoft.client_secret': { label: 'Microsoft client secret', kind: 'secret' },
  'microsoft.tenant': { label: 'Tenant', kind: 'text', help: 'common, organizations, consumers, or one tenant.' },
  'github.app_id': { label: 'GitHub App ID', kind: 'text' },
  'github.slug': { label: 'App slug', kind: 'text' },
  'github.client_id': { label: 'Client ID', kind: 'text' },
  'github.private_key': { label: 'Private key (PEM)', kind: 'pem' },
  'github.client_secret': { label: 'Client secret', kind: 'secret' },
  'github.webhook_secret': { label: 'Webhook secret', kind: 'secret' },
  'polar.access_token': { label: 'Polar access token', kind: 'secret', help: 'Set: billing is on.' },
  'polar.webhook_secret': { label: 'Polar webhook secret', kind: 'secret' },
  'polar.api_url': { label: 'Polar API address', kind: 'text' },
  'billing.alert_email': { label: 'Billing alert email', kind: 'text' },
  'retention.delete': { label: 'Delete workspaces locked for 12 months', kind: 'select', options: [['off', 'Off'], ['on', 'On']] },
  'retention.max_deletions_per_day': { label: 'Most deletions a day', kind: 'number' },
  'internal_signup.emails': { label: 'Team addresses', kind: 'text', help: '@domain or one mailbox, comma separated: their sign-ups are the team\'s own tests.' },
  'internal_signup.networks': { label: 'Team networks', kind: 'text', help: 'CIDR ranges, comma separated.' },
  'trusted_proxies': { label: 'Trusted proxies', kind: 'text', help: 'The reverse proxies whose X-Forwarded-For is believed.' },
  'cloud_auth.issuer': { label: 'Issuer', kind: 'text' },
  'cloud_auth.jwks_url': { label: 'JWKS address', kind: 'text' },
  'cloud_auth.authorized_parties': { label: 'Authorized parties', kind: 'text' },
  'cloud_auth.email_claim': { label: 'Email claim', kind: 'text' },
  'clerk.publishable_key': { label: 'Clerk publishable key', kind: 'text' },
  'help_center.domain': { label: 'Help center domain', kind: 'text', help: 'Each workspace gets <slug>.<domain>.' },
  'ai.allow_private_base_urls': { label: 'Let workspaces use private AI addresses', kind: 'switch' },
  'mail_image_cache.max_bytes': { label: 'Email image cache (bytes)', kind: 'number' },
  'kb_video.max_bytes': { label: 'Largest knowledge base video (bytes)', kind: 'number', help: 'Your reverse proxy must accept parts of 8 MiB.' },
  'problem_report.workspace': { label: 'Problem reports go to workspace', kind: 'text', help: "A workspace's address (slug); empty sends nothing." },
  'telemetry.enabled': { label: 'Usage report', kind: 'switch' },
  'operator.require_totp': { label: 'Require an authenticator', kind: 'switch', help: 'Can be turned off only on the server, never on the hosted cloud.' },
};

export const sections: Record<string, Section[]> = {
  ai: [
    { title: 'Server provider', lead: "Workspaces that bring no key of their own use this one.", keys: ['ai.server.provider', 'ai.server.label', 'ai.server.base_url', 'ai.server.api_key', 'ai.server.models'] },
    { title: 'Pricing and allowance', lead: 'Costs already counted keep the price of their day.', keys: ['ai.server.prices', 'ai.server.monthly_allowance'] },
  ],
  email: [
    { title: 'Outgoing mail (SMTP)', lead: 'Without a server, mail waits in the outbox and nothing is lost.', keys: ['smtp.host', 'smtp.port', 'smtp.username', 'smtp.password', 'smtp.starttls', 'email.from_policy'], test: 'smtp' },
    { title: 'Reply addresses', lead: 'Where replies to the server\'s own addresses come back.', keys: ['mail_from.domain', 'mail_from.inbound_secret', 'email.trusted_authserv_id', 'hosted_mail.domain'] },
    { title: 'Resend', lead: 'Delivery events and domain checks.', keys: ['resend.webhook_secret', 'resend.api_key'] },
  ],
  mailboxes: [
    { title: 'Google', lead: 'Lets workspaces connect Gmail.', keys: ['google.client_id', 'google.client_secret'] },
    { title: 'Microsoft 365', lead: 'Lets workspaces connect Microsoft 365 and Outlook.com.', keys: ['microsoft.client_id', 'microsoft.client_secret', 'microsoft.tenant'] },
    { title: 'GitHub App', lead: 'On when all six are set. Autofix works through it.', keys: ['github.app_id', 'github.slug', 'github.client_id', 'github.private_key', 'github.client_secret', 'github.webhook_secret'] },
  ],
  billing: [
    { title: 'Polar', lead: 'Billing turns the server into the hosted cloud: trials, locks and plans.', keys: ['polar.access_token', 'polar.webhook_secret', 'polar.api_url', 'billing.alert_email'], test: 'polar' },
    { title: 'Retention', lead: 'What happens to workspaces that stopped paying a year ago.', keys: ['retention.delete', 'retention.max_deletions_per_day'] },
  ],
  signups: [
    { title: 'Your own team', lead: 'Their sign-ups are left out of trial counts.', keys: ['internal_signup.emails', 'internal_signup.networks'] },
  ],
  security: [
    { title: 'Network', lead: 'Changing these is tried for 10 minutes before it stays.', keys: ['trusted_proxies', 'help_center.domain', 'ai.allow_private_base_urls'] },
    { title: 'Cloud sign-in', lead: 'Who may sign in to the hosted cloud.', keys: ['cloud_auth.issuer', 'cloud_auth.jwks_url', 'cloud_auth.authorized_parties', 'cloud_auth.email_claim', 'clerk.publishable_key'], test: 'jwks' },
    { title: 'Operator sign-in', lead: 'Turning it off is done on the server only.', keys: ['operator.require_totp'] },
  ],
  limits: [
    { title: 'Limits', lead: 'Sizes the server takes.', keys: ['mail_image_cache.max_bytes', 'kb_video.max_bytes'] },
    { title: 'Reports', lead: 'Problem reports and the daily usage report.', keys: ['problem_report.workspace', 'telemetry.enabled'] },
  ],
};

export const groupOrder = ['ai', 'email', 'mailboxes', 'billing', 'signups', 'security', 'limits'];
